import fs from "node:fs";
import { builtinModules } from "node:module";
import path from "node:path";
import { rolldown } from "rolldown";

const extensionRoot = path.resolve(import.meta.dirname, "..");
const repoRoot = path.resolve(extensionRoot, "..", "..");
const serverRoot = path.join(extensionRoot, "server", "language-server");
const serverEntry = path.join(repoRoot, "packages", "language-server", "dist", "server.js");
const nodeBuiltins = new Set([...builtinModules, ...builtinModules.map((name) => `node:${name}`)]);

if (!fs.existsSync(serverEntry)) {
  throw new Error(`Build @asp-lsp/language-server before packaging: ${serverEntry}`);
}

fs.rmSync(path.join(extensionRoot, "server"), { recursive: true, force: true });
fs.mkdirSync(path.join(serverRoot, "dist"), { recursive: true });

const bundle = await rolldown({
  input: serverEntry,
  platform: "node",
  resolve: {
    mainFields: ["module", "main"],
  },
  external: (id) => nodeBuiltins.has(id),
});

try {
  await bundle.write({
    file: path.join(serverRoot, "dist", "server.js"),
    format: "cjs",
    sourcemap: true,
    exports: "auto",
  });
} finally {
  await bundle.close();
}

fs.chmodSync(path.join(serverRoot, "dist", "server.js"), 0o755);
fs.writeFileSync(
  path.join(serverRoot, "package.json"),
  `${JSON.stringify(
    {
      name: "@asp-lsp/language-server-bundled",
      version: "0.1.0",
      private: true,
      main: "dist/server.js",
    },
    null,
    2,
  )}\n`,
);
