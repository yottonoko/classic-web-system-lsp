import { parentPort } from "node:worker_threads";
import type { BulkWorkerRequest, BulkWorkerResponse } from "./bulk-protocol";
import { SpillStore } from "./spill-store";

parentPort?.on("message", (request: BulkWorkerRequest) => {
  void handleRequest(request);
});

async function handleRequest(request: BulkWorkerRequest): Promise<void> {
  const startedAt = process.hrtime.bigint();
  try {
    if (request.kind === "spillRecord") {
      const store = new SpillStore({ directory: request.directory });
      const ref = await store.writeRecord(request.recordKind, request.record);
      postResponse({
        id: request.id,
        ref,
        runMs: elapsedMs(startedAt),
      });
      return;
    }
    postResponse({
      id: request.id,
      error: {
        message: "Unknown bulk worker request.",
      },
    });
  } catch (error) {
    postResponse({
      id: request.id,
      runMs: elapsedMs(startedAt),
      error: serializeError(error),
    });
  }
}

function postResponse(response: BulkWorkerResponse): void {
  parentPort?.postMessage(response);
}

function serializeError(error: unknown): BulkWorkerResponse["error"] {
  if (error instanceof Error) {
    return {
      name: error.name,
      message: error.message,
      stack: error.stack,
    };
  }
  return { message: String(error) };
}

function elapsedMs(startedAt: bigint): number {
  return Number(process.hrtime.bigint() - startedAt) / 1_000_000;
}
