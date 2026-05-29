import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

const repoRoot = path.resolve(import.meta.dirname, "..");
const wasmTarget = "wasm32-unknown-unknown";

execFileSync(
  "cargo",
  ["build", "--release", "-p", "asp-lsp-core", "--lib", "--target", wasmTarget],
  {
    cwd: repoRoot,
    stdio: "inherit",
  },
);

const source = path.join(repoRoot, "target", wasmTarget, "release", "asp_lsp_core.wasm");
const outDir = path.join(repoRoot, "packages", "core", "wasm");
fs.mkdirSync(outDir, { recursive: true });
fs.copyFileSync(source, path.join(outDir, "asp_lsp_core.wasm"));

console.log(`wasm core: ${path.relative(repoRoot, path.join(outDir, "asp_lsp_core.wasm"))}`);
