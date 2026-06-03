import { parentPort } from "node:worker_threads";
import { analyzeVbscriptFromTextAsync } from "@asp-lsp/core";
import type {
  VbDiagnosticsWorkerContext,
  VbDiagnosticsWorkerDocument,
  VbDiagnosticsWorkerRequest,
  VbDiagnosticsWorkerResponse,
  VbDiagnosticsWorkerTiming,
} from "./vb-diagnostics-protocol";
import type { AspParsedDocument, VbProjectContext } from "@asp-lsp/core";

if (!parentPort) {
  throw new Error("VBScript diagnostics worker requires a parent port.");
}

parentPort.on("message", async (request: VbDiagnosticsWorkerRequest) => {
  const timings: VbDiagnosticsWorkerTiming[] = [];
  try {
    const diagnostics = (
      await analyzeVbscriptFromTextAsync(request.uri, request.text, request.settings, {
        ...analysisContext(request.context),
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

function analysisContext(context: VbDiagnosticsWorkerContext): VbProjectContext {
  return {
    ...context,
    documents: context.documents?.map(workerDocumentToParsedDocument),
  };
}

function workerDocumentToParsedDocument(document: VbDiagnosticsWorkerDocument): AspParsedDocument {
  return {
    ...document,
    cst: {
      kind: "Document",
      start: 0,
      end: document.text.length,
      contentStart: 0,
      contentEnd: document.text.length,
      tokens: [],
      children: [],
    },
  };
}

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
