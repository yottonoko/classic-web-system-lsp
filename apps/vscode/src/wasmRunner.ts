import * as fs from "node:fs";
import * as path from "node:path";
import { WASI } from "node:wasi";

/**
 * Zero-dependency Node.js WASI runner for `classic-asp-lsp.wasm` (Any OS).
 * Usage: node dist/wasm-runner.js <path-to-classic-asp-lsp.wasm> [args...]
 */
async function main(): Promise<void> {
  const [, , wasmPath, ...extraArgs] = process.argv;
  if (!wasmPath) {
    process.stderr.write("Usage: wasm-runner.js <classic-asp-lsp.wasm> [args...]\n");
    process.exit(2);
  }

  const preopens: Record<string, string> = {};
  const rootsEnv = process.env.ASP_LSP_WASI_PREOPENS ?? "";
  for (const entry of rootsEnv.split(path.delimiter)) {
    const trimmed = entry.trim();
    if (trimmed.length > 0 && fs.existsSync(trimmed)) {
      preopens[trimmed] = trimmed;
    }
  }
  if (Object.keys(preopens).length === 0) {
    const cwd = process.cwd();
    preopens[cwd] = cwd;
  }
  if (process.platform !== "win32" && fs.existsSync("/")) {
    preopens["/"] = "/";
  }

  const wasi = new WASI({
    version: "preview1",
    args: ["classic-asp-lsp", ...extraArgs],
    env: process.env,
    preopens,
  });

  const wasmBytes = fs.readFileSync(wasmPath);
  const wasmModule = await WebAssembly.compile(wasmBytes);
  const instance = await WebAssembly.instantiate(
    wasmModule,
    wasi.getImportObject() as WebAssembly.Imports,
  );
  wasi.start(instance);
}

main().catch((err: unknown) => {
  const detail = err instanceof Error && err.stack ? err.stack : String(err);
  process.stderr.write(`classic-asp-lsp wasm runner error: ${detail}\n`);
  process.exit(1);
});
