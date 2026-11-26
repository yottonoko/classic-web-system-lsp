import * as path from "node:path";
import { build } from "rolldown";

const rootDir = path.resolve(import.meta.dirname, "..");

await build({
  input: path.join(rootDir, "src", "extension.ts"),
  external: ["vscode"],
  platform: "node",
  output: {
    file: path.join(rootDir, "dist", "extension.js"),
    format: "cjs",
    sourcemap: true,
  },
});

await build({
  input: path.join(rootDir, "src", "wasmRunner.ts"),
  platform: "node",
  output: {
    file: path.join(rootDir, "dist", "wasm-runner.js"),
    format: "cjs",
    sourcemap: true,
  },
});
