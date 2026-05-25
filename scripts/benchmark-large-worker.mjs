import { parentPort } from "node:worker_threads";
import { performance } from "node:perf_hooks";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const {
  analyzeVbscript,
  buildVirtualDocuments,
  collectVbscriptSymbols,
  parseAspDocument,
} = require("../packages/core/dist/index.js");

if (!parentPort) {
  throw new Error("benchmark worker requires a parent port.");
}

parentPort.on("message", (message) => {
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
    const parsed =
      message.operation === "parseAspDocument"
        ? undefined
        : parseAspDocument(message.source.uri, message.source.text);
    if (message.operation === "parseAspDocument") {
      parseAspDocument(message.source.uri, message.source.text);
    } else if (message.operation === "buildVirtualDocuments") {
      buildVirtualDocuments(parsed);
    } else if (message.operation === "collectVbscriptSymbols") {
      collectVbscriptSymbols(parsed);
    } else if (message.operation === "analyzeVbscript") {
      analyzeVbscript(parsed, context);
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
