import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

export function readBenchmarkCacheMode() {
  const raw = (process.env.ASP_LSP_BENCH_CACHE_MODE ?? "hot").toLowerCase();
  if (raw === "cold" || raw === "hot") {
    return raw;
  }
  throw new Error("ASP_LSP_BENCH_CACHE_MODE must be cold or hot.");
}

export function benchmarkSourcesForRun(sources, cacheMode, operation, run) {
  if (cacheMode !== "cold") {
    return sources;
  }
  const token = benchmarkRunToken(operation, run);
  return sources.map((source) => ({
    ...source,
    uri: syntheticBenchmarkUri(source.uri, token),
  }));
}

function benchmarkRunToken(operation, run) {
  const safeOperation = operation.replace(/[^A-Za-z0-9_-]/g, "_");
  return `__asp_lsp_bench_${safeOperation}_${run.phase}_${run.index}`;
}

function syntheticBenchmarkUri(uri, token) {
  if (uri.startsWith("file://")) {
    const filePath = fileURLToPath(uri);
    return pathToFileURL(insertTokenBeforeExtension(filePath, token)).href;
  }
  const splitIndex = uri.search(/[?#]/);
  const base = splitIndex === -1 ? uri : uri.slice(0, splitIndex);
  const suffix = splitIndex === -1 ? "" : uri.slice(splitIndex);
  return `${insertTokenBeforeExtension(base, token)}${suffix}`;
}

function insertTokenBeforeExtension(value, token) {
  const extension = path.extname(value);
  if (!extension) {
    return `${value}.${token}`;
  }
  return `${value.slice(0, -extension.length)}.${token}${extension}`;
}
