import { parentPort } from "node:worker_threads";
import { analyzeVbscriptAsync, aspAnalysisBackendInfo } from "@asp-lsp/core";
import type {
  VbDiagnosticsWorkerRequest,
  VbDiagnosticsWorkerResponse,
  VbDiagnosticsWorkerTiming,
} from "./vb-diagnostics-protocol";

if (!parentPort) {
  throw new Error("VBScript diagnostics worker requires a parent port.");
}

parentPort.on("message", async (request: VbDiagnosticsWorkerRequest) => {
  const timings: VbDiagnosticsWorkerTiming[] = [];
  try {
    const diagnostics = (
      await analyzeVbscriptAsync(request.parsed, {
        ...request.context,
        debugStep: (name, action) => {
          const startedAt = process.hrtime.bigint();
          try {
            return action();
          } finally {
            timings.push({
              name,
              elapsedMs: Number(process.hrtime.bigint() - startedAt) / 1_000_000,
            });
          }
        },
      })
    ).diagnostics;
    const backend = aspAnalysisBackendInfo();
    timings.push({
      name: `backend.${backend.backend}`,
      elapsedMs: 0,
    });
    parentPort?.postMessage({
      id: request.id,
      diagnostics,
      timings,
    } satisfies VbDiagnosticsWorkerResponse);
  } catch (error) {
    parentPort?.postMessage({
      id: request.id,
      error: serializeWorkerError(error),
    } satisfies VbDiagnosticsWorkerResponse);
  }
});

function serializeWorkerError(error: unknown): VbDiagnosticsWorkerResponse["error"] {
  if (error instanceof Error) {
    return {
      name: error.name,
      message: error.message,
      stack: error.stack,
    };
  }
  return {
    message: String(error),
  };
}
