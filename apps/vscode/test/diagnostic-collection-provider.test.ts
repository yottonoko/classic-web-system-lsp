import fs from "node:fs";
import { describe, expect, it, vi } from "vitest";
import type * as vscode from "vscode";
import type { DiagnosticCollectionSource } from "vscode-languageclient/node";
import { SharedDiagnosticCollectionProvider } from "../src/diagnostic-collection-provider";

const push = "push" as DiagnosticCollectionSource;
const pull = "pull" as DiagnosticCollectionSource;

vi.mock("vscode", () => ({
  languages: {
    createDiagnosticCollection: vi.fn(),
  },
}));

interface FakeCollection {
  name: string;
  dispose: ReturnType<typeof vi.fn>;
}

function fakeCollection(name: string | undefined): FakeCollection {
  return { name: name ?? "", dispose: vi.fn() };
}

function createProvider() {
  const createCollection = vi.fn((name: string | undefined) => fakeCollection(name));
  const provider = new SharedDiagnosticCollectionProvider(
    createCollection as unknown as (name: string | undefined) => vscode.DiagnosticCollection,
  );
  return { createCollection, provider };
}

describe("SharedDiagnosticCollectionProvider", () => {
  it("returns one collection for push and pull sources", () => {
    const { createCollection, provider } = createProvider();

    const pushCollection = provider.create("asp-lsp", push);
    const pullCollection = provider.create(undefined, pull);

    expect(pushCollection).toBe(pullCollection);
    expect(createCollection).toHaveBeenCalledTimes(1);
    expect(createCollection).toHaveBeenCalledWith("asp-lsp");
  });

  it("keeps source releases from disposing the persistent collection", () => {
    const { provider } = createProvider();
    const collection = provider.create("asp-lsp", push) as unknown as FakeCollection;
    provider.create(undefined, pull);

    provider.dispose(collection, push);
    provider.dispose(collection, pull);
    provider.dispose(collection, pull);

    expect(collection.dispose).not.toHaveBeenCalled();

    provider.dispose();
    expect(collection.dispose).toHaveBeenCalledTimes(1);
    provider.dispose(collection, push);
    expect(collection.dispose).toHaveBeenCalledTimes(1);
  });
});

class SimulatedLanguageClient {
  private pushCollection: FakeCollection | undefined;
  private pullCollection: FakeCollection | undefined;

  constructor(
    private readonly provider: SharedDiagnosticCollectionProvider,
    private readonly failStart = false,
  ) {}

  get collection(): FakeCollection {
    if (!this.pushCollection || !this.pullCollection) {
      throw new Error("client has not acquired diagnostics");
    }
    if (this.pushCollection !== this.pullCollection) {
      throw new Error("push and pull collections differ");
    }
    return this.pushCollection;
  }

  async start(): Promise<void> {
    this.pushCollection = this.provider.create("asp-lsp", push) as unknown as FakeCollection;
    this.pullCollection = this.provider.create(undefined, pull) as unknown as FakeCollection;
    if (this.failStart) {
      throw new Error("simulated startup failure");
    }
  }

  async dispose(): Promise<void> {
    if (this.pushCollection) {
      this.provider.dispose(this.pushCollection, push);
    }
    if (this.pullCollection) {
      this.provider.dispose(this.pullCollection, pull);
    }
  }
}

describe("diagnostic collection client lifetimes", () => {
  it("retains identity across failed and sequential client restarts until final disposal", async () => {
    const { createCollection, provider } = createProvider();
    const failedClient = new SimulatedLanguageClient(provider, true);

    await expect(failedClient.start()).rejects.toThrow("simulated startup failure");
    const collection = failedClient.collection;
    await failedClient.dispose();
    expect(collection.dispose).not.toHaveBeenCalled();

    const restartedClient = new SimulatedLanguageClient(provider);
    await restartedClient.start();
    expect(restartedClient.collection).toBe(collection);
    expect(createCollection).toHaveBeenCalledTimes(1);

    // A delayed release from the failed client must not affect the restarted client.
    await failedClient.dispose();
    expect(collection.dispose).not.toHaveBeenCalled();

    await restartedClient.dispose();
    expect(collection.dispose).not.toHaveBeenCalled();

    provider.dispose();
    expect(collection.dispose).toHaveBeenCalledTimes(1);
    provider.dispose();
    expect(collection.dispose).toHaveBeenCalledTimes(1);
  });
});

describe("diagnostic collection client wiring", () => {
  it("uses the shared provider for every client lifetime and keeps pull diagnostics enabled", () => {
    const extensionSource = fs.readFileSync("src/extension.ts", "utf8");
    const startSource = extensionSource.slice(
      extensionSource.indexOf("async function startClient"),
      extensionSource.indexOf("export async function deactivate"),
    );
    const deactivateSource = extensionSource.slice(
      extensionSource.indexOf("export async function deactivate"),
      extensionSource.indexOf("export async function synchronizeAspLspConfiguration"),
    );

    expect(extensionSource).toContain(
      'import { SharedDiagnosticCollectionProvider } from "./diagnostic-collection-provider"',
    );
    expect(extensionSource).toContain(
      "diagnosticCollectionProvider = new SharedDiagnosticCollectionProvider()",
    );
    expect(startSource).toContain(
      "diagnosticCollectionProvider: activeDiagnosticCollectionProvider",
    );
    expect(startSource).not.toContain("provideDiagnostics:");
    expect(deactivateSource).toContain("activeDiagnosticCollectionProvider?.dispose()");
    expect(deactivateSource).toContain("diagnosticCollectionProvider = undefined");
    const restartSource = extensionSource.slice(
      extensionSource.indexOf("async function restartServerOnce"),
      extensionSource.indexOf("export async function synchronizeAspLspConfiguration"),
    );
    expect(restartSource).not.toContain("diagnosticCollectionProvider?.dispose()");
  });
});
