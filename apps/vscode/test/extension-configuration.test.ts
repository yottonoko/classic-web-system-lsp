import { describe, expect, it, vi } from "vitest";
import type { ConfigurationRequest, LanguageClient } from "vscode-languageclient/node";

const vscodeMock = vi.hoisted(() => ({
  workspace: {
    isTrusted: false,
    getConfiguration: () => ({ get: () => ({}) }),
  },
}));

vi.mock("vscode", () => vscodeMock);
vi.mock("vscode-languageclient/node", () => ({
  LanguageClient: class LanguageClient {},
}));

import {
  synchronizeAspLspConfiguration,
  workspaceConfigurationMiddleware,
  workspaceSafeAspLspConfiguration,
} from "../src/extension";

describe("workspaceSafeAspLspConfiguration", () => {
  it("deep-copies configuration and clears filesystem-expanding values while untrusted", () => {
    const configured = {
      includePaths: ["/outside/includes"],
      virtualRoot: "/outside/virtual",
      virtualRoots: ["/outside/virtual-roots"],
      workspace: { includes: ["**/*.asp"] },
      cache: { enabled: true, directory: "/outside/cache", freshness: "watch" },
      debug: {
        output: "verbose",
        logFile: { enabled: true, path: "/outside/debug.log" },
      },
    };
    const safe = workspaceSafeAspLspConfiguration(configured, false);

    expect(safe).toEqual({
      includePaths: [],
      virtualRoot: "",
      virtualRoots: [],
      workspace: { includes: ["**/*.asp"] },
      cache: { enabled: true, directory: "", freshness: "watch" },
      debug: {
        output: "verbose",
        logFile: { enabled: false, path: "" },
      },
    });
    const safeRecord = safe as Record<string, unknown>;
    expect(safe).not.toBe(configured);
    expect(safeRecord.workspace).not.toBe(configured.workspace);
    expect((safeRecord.workspace as Record<string, unknown>).includes).not.toBe(
      configured.workspace.includes,
    );
    expect(safeRecord.cache).not.toBe(configured.cache);
    expect(safeRecord.debug).not.toBe(configured.debug);
    expect((safeRecord.debug as Record<string, unknown>).logFile).not.toBe(
      configured.debug.logFile,
    );
    expect(configured).toEqual({
      includePaths: ["/outside/includes"],
      virtualRoot: "/outside/virtual",
      virtualRoots: ["/outside/virtual-roots"],
      workspace: { includes: ["**/*.asp"] },
      cache: { enabled: true, directory: "/outside/cache", freshness: "watch" },
      debug: {
        output: "verbose",
        logFile: { enabled: true, path: "/outside/debug.log" },
      },
    });
  });

  for (const [label, malformed] of [
    ["null", null],
    ["undefined", undefined],
    ["string", "/outside/settings.json"],
    ["number", 42],
    ["boolean", true],
    ["array", ["/outside/settings.json"]],
  ] as const) {
    it(`returns fresh explicit clears for an untrusted ${label} root`, () => {
      const first = workspaceSafeAspLspConfiguration(malformed, false);
      const second = workspaceSafeAspLspConfiguration(malformed, false);

      expect(first).toEqual({
        includePaths: [],
        virtualRoot: "",
        virtualRoots: [],
        cache: { directory: "" },
        debug: { logFile: { enabled: false, path: "" } },
      });
      expect(first).not.toBe(malformed);
      expect(first).not.toBe(second);
      expect((first as Record<string, unknown>).includePaths).not.toBe(
        (second as Record<string, unknown>).includePaths,
      );
    });
  }

  it("emits nested clears when untrusted settings omit cache and debug objects", () => {
    const configured = {
      workspace: { includes: ["**/*.asp"] },
    };

    expect(workspaceSafeAspLspConfiguration(configured, false)).toEqual({
      workspace: { includes: ["**/*.asp"] },
      includePaths: [],
      virtualRoot: "",
      virtualRoots: [],
      cache: { directory: "" },
      debug: { logFile: { enabled: false, path: "" } },
    });
    expect(configured).toEqual({ workspace: { includes: ["**/*.asp"] } });
  });

  it("preserves the configured object after workspace trust is granted", () => {
    const configured = {
      includePaths: ["/outside/includes"],
      virtualRoot: "/outside/virtual",
      virtualRoots: ["/outside/virtual-roots"],
      cache: { directory: "/outside/cache" },
      debug: { logFile: { enabled: true, path: "/outside/debug.log" } },
    };

    expect(workspaceSafeAspLspConfiguration(configured, true)).toBe(configured);
  });
});

describe("workspace configuration integration", () => {
  it("sanitizes aspLsp values returned by a workspace/configuration pull", async () => {
    const params: Parameters<ConfigurationRequest.MiddlewareSignature>[0] = {
      items: [{ section: "aspLsp" }, { section: "other" }],
    };
    const token = {} as Parameters<ConfigurationRequest.MiddlewareSignature>[1];
    const aspLsp = {
      includePaths: ["/outside/includes"],
      virtualRoot: "/outside/virtual",
      virtualRoots: ["/outside/virtual-roots"],
      workspace: { includes: ["**/*.asp"] },
      cache: { enabled: true, directory: "/outside/cache", freshness: "watch" },
      debug: {
        output: "verbose",
        logFile: { enabled: true, path: "/outside/debug.log" },
      },
    };
    const other = { includePaths: ["/other"] };
    const next = vi.fn<ConfigurationRequest.HandlerSignature>().mockResolvedValue([aspLsp, other]);

    const result = await workspaceConfigurationMiddleware(params, token, next);

    expect(next).toHaveBeenCalledWith(params, token);
    expect(result).toEqual([
      {
        includePaths: [],
        virtualRoot: "",
        virtualRoots: [],
        workspace: { includes: ["**/*.asp"] },
        cache: { enabled: true, directory: "", freshness: "watch" },
        debug: {
          output: "verbose",
          logFile: { enabled: false, path: "" },
        },
      },
      other,
    ]);
    expect(aspLsp).toEqual({
      includePaths: ["/outside/includes"],
      virtualRoot: "/outside/virtual",
      virtualRoots: ["/outside/virtual-roots"],
      workspace: { includes: ["**/*.asp"] },
      cache: { enabled: true, directory: "/outside/cache", freshness: "watch" },
      debug: {
        output: "verbose",
        logFile: { enabled: true, path: "/outside/debug.log" },
      },
    });
  });

  it("adds nested clears to an untrusted pull when cache and debug settings are absent", async () => {
    const params: Parameters<ConfigurationRequest.MiddlewareSignature>[0] = {
      items: [{ section: "aspLsp" }],
    };
    const token = {} as Parameters<ConfigurationRequest.MiddlewareSignature>[1];
    const next = vi
      .fn<ConfigurationRequest.HandlerSignature>()
      .mockResolvedValue([{ workspace: { includes: ["**/*.asp"] } }]);

    await expect(workspaceConfigurationMiddleware(params, token, next)).resolves.toEqual([
      {
        workspace: { includes: ["**/*.asp"] },
        includePaths: [],
        virtualRoot: "",
        virtualRoots: [],
        cache: { directory: "" },
        debug: { logFile: { enabled: false, path: "" } },
      },
    ]);
  });

  it("clears previously trusted state when an untrusted pull returns a null root", async () => {
    const params: Parameters<ConfigurationRequest.MiddlewareSignature>[0] = {
      items: [{ section: "aspLsp" }],
    };
    const token = {} as Parameters<ConfigurationRequest.MiddlewareSignature>[1];
    const next = vi.fn<ConfigurationRequest.HandlerSignature>().mockResolvedValue([null]);

    await expect(workspaceConfigurationMiddleware(params, token, next)).resolves.toEqual([
      {
        includePaths: [],
        virtualRoot: "",
        virtualRoots: [],
        cache: { directory: "" },
        debug: { logFile: { enabled: false, path: "" } },
      },
    ]);
  });

  it("clears trusted filesystem settings when trust is revoked before resynchronization", async () => {
    const configured = {
      includePaths: ["/outside/includes"],
      virtualRoot: "/outside/virtual",
      virtualRoots: ["/outside/virtual-roots"],
      workspace: { includes: ["**/*.asp"] },
      cache: { enabled: true, directory: "/outside/cache", freshness: "watch" },
      debug: {
        output: "verbose",
        logFile: { enabled: true, path: "/outside/debug.log" },
      },
    };
    vscodeMock.workspace.getConfiguration = () => ({ get: () => configured });
    const sendNotification = vi.fn<LanguageClient["sendNotification"]>().mockResolvedValue();

    vscodeMock.workspace.isTrusted = true;
    try {
      await synchronizeAspLspConfiguration({ sendNotification } as unknown as LanguageClient);
      vscodeMock.workspace.isTrusted = false;
      await synchronizeAspLspConfiguration({ sendNotification } as unknown as LanguageClient);
    } finally {
      vscodeMock.workspace.isTrusted = false;
    }

    expect(sendNotification).toHaveBeenNthCalledWith(1, "workspace/didChangeConfiguration", {
      settings: { aspLsp: configured },
    });
    expect(sendNotification).toHaveBeenNthCalledWith(2, "workspace/didChangeConfiguration", {
      settings: {
        aspLsp: {
          includePaths: [],
          virtualRoot: "",
          virtualRoots: [],
          workspace: { includes: ["**/*.asp"] },
          cache: { enabled: true, directory: "", freshness: "watch" },
          debug: {
            output: "verbose",
            logFile: { enabled: false, path: "" },
          },
        },
      },
    });
  });

  it("sends nested clears when resynchronizing an untrusted workspace without those objects", async () => {
    const configured = { workspace: { includes: ["**/*.asp"] } };
    vscodeMock.workspace.getConfiguration = () => ({ get: () => configured });
    const sendNotification = vi.fn<LanguageClient["sendNotification"]>().mockResolvedValue();

    await synchronizeAspLspConfiguration({ sendNotification } as unknown as LanguageClient);

    expect(sendNotification).toHaveBeenCalledWith("workspace/didChangeConfiguration", {
      settings: {
        aspLsp: {
          workspace: { includes: ["**/*.asp"] },
          includePaths: [],
          virtualRoot: "",
          virtualRoots: [],
          cache: { directory: "" },
          debug: { logFile: { enabled: false, path: "" } },
        },
      },
    });
  });

  it("sends explicit clears when an untrusted pushed resynchronization has a null root", async () => {
    vscodeMock.workspace.getConfiguration = () => ({ get: () => null });
    const sendNotification = vi.fn<LanguageClient["sendNotification"]>().mockResolvedValue();

    await synchronizeAspLspConfiguration({ sendNotification } as unknown as LanguageClient);

    expect(sendNotification).toHaveBeenCalledWith("workspace/didChangeConfiguration", {
      settings: {
        aspLsp: {
          includePaths: [],
          virtualRoot: "",
          virtualRoots: [],
          cache: { directory: "" },
          debug: { logFile: { enabled: false, path: "" } },
        },
      },
    });
  });

  it("resends configured roots after workspace trust is granted", async () => {
    const configured = {
      includePaths: ["/trusted/includes"],
      virtualRoot: "/trusted/virtual",
      virtualRoots: ["/trusted/virtual-roots"],
    };
    vscodeMock.workspace.isTrusted = true;
    vscodeMock.workspace.getConfiguration = () => ({ get: () => configured });
    const sendNotification = vi.fn<LanguageClient["sendNotification"]>().mockResolvedValue();

    try {
      await synchronizeAspLspConfiguration({ sendNotification } as unknown as LanguageClient);
    } finally {
      vscodeMock.workspace.isTrusted = false;
    }

    expect(sendNotification).toHaveBeenCalledWith("workspace/didChangeConfiguration", {
      settings: { aspLsp: configured },
    });
  });
});
