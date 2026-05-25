import { parentPort } from "node:worker_threads";
import { analyzeVbscript, type AspParsedDocument, type VbProjectContext } from "@asp-lsp/core";
import type { Diagnostic } from "vscode-languageserver/node";

interface VbDiagnosticsWorkerTiming {
  step: string;
  elapsedMs: number;
}

interface VbDiagnosticsWorkerRequest {
  id: number;
  parsed: AspParsedDocument;
  context: VbProjectContext;
}

if (!parentPort) {
  throw new Error("VBScript diagnostics worker requires a parent port.");
}

parentPort.on("message", (message: VbDiagnosticsWorkerRequest) => {
  const timings: VbDiagnosticsWorkerTiming[] = [];
  try {
    const diagnostics = analyzeVbscript(message.parsed, {
      ...message.context,
      debugStep: (step, action) => {
        const startedAt = process.hrtime.bigint();
        try {
          return action();
        } finally {
          timings.push({
            step,
            elapsedMs: Number(process.hrtime.bigint() - startedAt) / 1_000_000,
          });
        }
      },
    }).diagnostics;
    parentPort?.postMessage({
      id: message.id,
      diagnostics,
      timings,
    } satisfies { id: number; diagnostics: Diagnostic[]; timings: VbDiagnosticsWorkerTiming[] });
  } catch (error) {
    parentPort?.postMessage({
      id: message.id,
      error: error instanceof Error ? error.message : String(error),
    });
  }
});
