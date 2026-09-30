import { describe, expect, it, vi } from "vitest";

const progressSessions = vi.hoisted(
  () => [] as Array<Array<{ increment?: number; message?: string }>>,
);
const vscodeMock = vi.hoisted(() => ({
  Uri: {
    parse(value: string) {
      return { toString: () => value };
    },
  },
  window: {
    activeTextEditor: undefined,
    withProgress: vi.fn(
      (
        _options: unknown,
        operation: (
          progress: { report(value: { increment?: number; message?: string }): void },
          token: { isCancellationRequested: boolean },
        ) => unknown,
      ) => {
        const reports: Array<{ increment?: number; message?: string }> = [];
        progressSessions.push(reports);
        return operation(
          { report: (value) => reports.push(value) },
          { isCancellationRequested: false },
        );
      },
    ),
  },
}));

vi.mock("vscode", () => vscodeMock);
vi.mock("vscode-languageclient/node", () => ({
  CloseAction: { DoNotRestart: 1, Restart: 2 },
  ErrorAction: { Continue: 1, Shutdown: 2 },
}));

import { createProgressController } from "../src/extension-progress";

describe("extension progress controller", () => {
  it("shows discovered counts and phase changes during workspace analysis", async () => {
    progressSessions.length = 0;
    const controller = createProgressController({
      getClient: () => undefined,
      getStatusBarItem: () => ({ text: "", tooltip: "" }) as never,
      isDeactivating: () => false,
      isManualRestarting: () => false,
      localize: () => (key) => key,
      locale: () => "en",
      baseNameFromPath: () => undefined,
      baseNameFromUri: () => undefined,
    });
    let finish: (() => void) | undefined;
    const operation = controller.withServerTaskProgress(
      {} as never,
      { labelPrefixes: ["workspace.index"] },
      () => new Promise<void>((resolve) => (finish = resolve)),
    );
    await Promise.resolve();
    const report = (label: string, current: number, total: number) =>
      controller.handleServerStatusNotification({
        status: "analyzing",
        tasks: [
          {
            id: "workspace-index",
            kind: "loading",
            label,
            detail: "page.asp",
            current,
            total,
            state: "running",
            startedAt: Date.now() + 10,
            updatedAt: Date.now() + 10,
          },
        ],
      });
    report("workspace.index.scanRoot", 17, 0);
    expect(progressSessions[0]?.at(-1)?.message).toContain("17/?");
    report("workspace.index.scanFiles", 0, 20);
    expect(progressSessions[0]?.at(-1)?.message).toContain("0/20 (0%)");
    report("workspace.index.parseFiles", 8, 20);
    expect(progressSessions[0]?.at(-1)?.message).toContain("workspaceIndexParseFiles");
    expect(progressSessions[0]?.at(-1)?.message).toContain("8/20 (40%)");
    report("workspace.index.waitDocuments", 0, 0);
    expect(progressSessions[0]?.at(-1)?.message).toContain("workspaceIndexWaitDocuments");
    finish?.();
    await operation;
  });

  it("drops stale reporter claims when server state resets", async () => {
    progressSessions.length = 0;
    const controller = createProgressController({
      getClient: () => undefined,
      getStatusBarItem: () => ({ text: "", tooltip: "" }) as never,
      isDeactivating: () => false,
      isManualRestarting: () => false,
      localize: () => (key) => key,
      locale: () => "en",
      baseNameFromPath: () => undefined,
      baseNameFromUri: () => undefined,
    });
    let finishFirst: (() => void) | undefined;
    let finishSecond: (() => void) | undefined;
    const firstOperation = controller.withServerTaskProgress(
      {} as never,
      { labelPrefixes: ["flowchart."] },
      () => new Promise<void>((resolve) => (finishFirst = resolve)),
    );
    await Promise.resolve();
    controller.handleServerStatusNotification(progressStatus("flowchart-1", "flowchart.build"));
    expect(progressSessions[0]?.at(-1)?.message).toContain("flowchart-1");

    controller.resetServerState();
    const secondOperation = controller.withServerTaskProgress(
      {} as never,
      { labelPrefixes: ["flowchart."] },
      () => new Promise<void>((resolve) => (finishSecond = resolve)),
    );
    await Promise.resolve();
    controller.handleServerStatusNotification(progressStatus("flowchart-2", "flowchart.finalize"));

    expect(progressSessions[0]?.some((report) => report.message?.includes("flowchart-2"))).toBe(
      false,
    );
    expect(progressSessions[1]?.at(-1)?.message).toContain("flowchart-2");
    finishFirst?.();
    finishSecond?.();
    await Promise.all([firstOperation, secondOperation]);
  });

  it("reports the crash limit once instead of restarting again", () => {
    const limits: { count: number; minutes: number }[] = [];
    const controller = createProgressController({
      getClient: () => undefined,
      getStatusBarItem: () => ({ text: "", tooltip: "" }) as never,
      isDeactivating: () => false,
      isManualRestarting: () => false,
      onServerCrashLimit: (crash) => limits.push(crash),
      localize: () => (key) => key,
      locale: () => "en",
      baseNameFromPath: () => undefined,
      baseNameFromUri: () => undefined,
    });
    const handler = controller.createLanguageClientErrorHandler();
    const results = Array.from({ length: 5 }, () => handler.closed());

    expect(results.slice(0, 4).every((result) => "action" in result && result.action === 2)).toBe(
      true,
    );
    expect(results[4]).toEqual({ action: 1, handled: true });
    expect(limits).toEqual([{ count: 5, minutes: 3 }]);
    // The counter restarts so a manual restart gets the full retry budget again.
    expect(handler.closed()).toEqual({ action: 2 });
  });

  it("drops stale reporter claims before an automatic crash restart", async () => {
    progressSessions.length = 0;
    const controller = createProgressController({
      getClient: () => undefined,
      getStatusBarItem: () => ({ text: "", tooltip: "" }) as never,
      isDeactivating: () => false,
      isManualRestarting: () => false,
      localize: () => (key) => key,
      locale: () => "en",
      baseNameFromPath: () => undefined,
      baseNameFromUri: () => undefined,
    });
    let finishFirst: (() => void) | undefined;
    let finishSecond: (() => void) | undefined;
    const firstOperation = controller.withServerTaskProgress(
      {} as never,
      { labelPrefixes: ["flowchart."] },
      () => new Promise<void>((resolve) => (finishFirst = resolve)),
    );
    await Promise.resolve();
    controller.handleServerStatusNotification(
      progressStatus("flowchart-crashed", "flowchart.build"),
    );
    expect(progressSessions[0]?.at(-1)?.message).toContain("flowchart-crashed");

    controller.createLanguageClientErrorHandler().closed();
    const secondOperation = controller.withServerTaskProgress(
      {} as never,
      { labelPrefixes: ["flowchart."] },
      () => new Promise<void>((resolve) => (finishSecond = resolve)),
    );
    await Promise.resolve();
    controller.handleServerStatusNotification(
      progressStatus("flowchart-restarted", "flowchart.finalize"),
    );

    expect(
      progressSessions[0]?.some((report) => report.message?.includes("flowchart-restarted")),
    ).toBe(false);
    expect(progressSessions[1]?.at(-1)?.message).toContain("flowchart-restarted");
    finishFirst?.();
    finishSecond?.();
    await Promise.all([firstOperation, secondOperation]);
  });
});

function progressStatus(id: string, label: string): unknown {
  const now = Date.now() + 10;
  return {
    status: "analyzing",
    tasks: [
      {
        id,
        kind: "analyzing",
        label,
        detail: id,
        current: 1,
        total: 2,
        state: "running",
        startedAt: now,
        updatedAt: now,
      },
    ],
  };
}
