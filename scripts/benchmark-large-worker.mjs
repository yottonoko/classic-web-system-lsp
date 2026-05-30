import { parentPort } from "node:worker_threads";
import { performance } from "node:perf_hooks";
import { createRequire } from "node:module";
import { embeddedOperationNames, runEmbeddedOperation } from "./embedded-language-benchmark.mjs";

const require = createRequire(import.meta.url);
const {
  analyzeVbscriptFromTextAsync,
  buildVirtualDocuments,
  collectVbscriptSymbolsFromTextAsync,
  parseAspDocumentAsync,
} = require("../packages/core/dist/index.js");
const core = require("../packages/core/dist/index.js");

if (!parentPort) {
  throw new Error("benchmark worker requires a parent port.");
}

parentPort.on("message", async (message) => {
  const timings = new Map();
  const context = message.debugSteps
    ? {
        debugStep(name, action) {
          const start = performance.now();
          const result = action();
          timings.set(name, (timings.get(name) ?? 0) + performance.now() - start);
          return result;
        },
      }
    : {};
  try {
    if (message.operation === "parseAspDocument") {
      await parseAspDocumentAsync(message.source.uri, message.source.text);
    } else if (message.operation === "buildVirtualDocuments") {
      const parsed = await parseAspDocumentAsync(message.source.uri, message.source.text);
      buildVirtualDocuments(parsed);
    } else if (message.operation === "collectVbscriptSymbols") {
      await collectVbscriptSymbolsFromTextAsync(message.source.uri, message.source.text);
    } else if (message.operation === "analyzeVbscript") {
      await analyzeVbscriptFromTextAsync(message.source.uri, message.source.text, {}, context);
    } else if (embeddedOperationNames.includes(message.operation)) {
      await runEmbeddedOperation(message.operation, message.source, core);
    } else {
      throw new Error(`Unknown benchmark operation: ${message.operation}`);
    }
    parentPort?.postMessage({
      id: message.id,
      timings: [...timings.entries()],
    });
  } catch (error) {
    parentPort?.postMessage({
      id: message.id,
      error: error instanceof Error ? error.message : String(error),
    });
  }
});
