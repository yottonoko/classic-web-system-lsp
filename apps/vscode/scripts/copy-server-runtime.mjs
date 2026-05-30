import fs from "node:fs";
import { builtinModules, createRequire } from "node:module";
import path from "node:path";
import { rolldown } from "rolldown";

const extensionRoot = path.resolve(import.meta.dirname, "..");
const repoRoot = path.resolve(extensionRoot, "..", "..");
const serverRoot = path.join(extensionRoot, "server", "language-server");
const serverEntry = path.join(repoRoot, "packages", "language-server", "dist", "server.js");
const workerEntry = path.join(
  repoRoot,
  "packages",
  "language-server",
  "dist",
  "vb-diagnostics-worker.js",
);
const nodeBuiltins = new Set([...builtinModules, ...builtinModules.map((name) => `node:${name}`)]);
const require = createRequire(import.meta.url);
const languageServerManifest = JSON.parse(
  fs.readFileSync(path.join(repoRoot, "packages", "language-server", "package.json"), "utf8"),
);

if (!fs.existsSync(serverEntry)) {
  throw new Error(`Build @asp-lsp/language-server before packaging: ${serverEntry}`);
}
if (!fs.existsSync(workerEntry)) {
  throw new Error(`Build @asp-lsp/language-server before packaging: ${workerEntry}`);
}

fs.rmSync(path.join(extensionRoot, "server"), { recursive: true, force: true });
const distRoot = path.join(serverRoot, "dist");
fs.mkdirSync(distRoot, { recursive: true });

await bundleNodeEntry(serverEntry, path.join(distRoot, "server.js"));
await bundleNodeEntry(workerEntry, path.join(distRoot, "vb-diagnostics-worker.js"));
fs.chmodSync(path.join(distRoot, "server.js"), 0o755);
fs.chmodSync(path.join(distRoot, "vb-diagnostics-worker.js"), 0o755);
copyTypeScriptLibs(distRoot);
copyNativeCore(serverRoot);
fs.writeFileSync(
  path.join(serverRoot, "package.json"),
  `${JSON.stringify(
    {
      name: "@asp-lsp/language-server-bundled",
      version: languageServerManifest.version,
      private: true,
      main: "dist/server.js",
    },
    null,
    2,
  )}\n`,
);

async function bundleNodeEntry(input, output) {
  const bundle = await rolldown({
    input,
    platform: "node",
    resolve: {
      mainFields: ["module", "main"],
    },
    external: (id) => nodeBuiltins.has(id),
  });

  try {
    await bundle.write({
      file: output,
      format: "cjs",
      sourcemap: true,
      exports: "auto",
    });
  } finally {
    await bundle.close();
  }
}

function copyTypeScriptLibs(targetDirectory) {
  const typescriptPackage = require.resolve("typescript/package.json", { paths: [repoRoot] });
  const sourceDirectory = path.join(path.dirname(typescriptPackage), "lib");
  for (const entry of fs.readdirSync(sourceDirectory)) {
    if (/^lib\..*\.d\.ts$/.test(entry)) {
      fs.copyFileSync(path.join(sourceDirectory, entry), path.join(targetDirectory, entry));
    }
  }
}

function copyNativeCore(targetRoot) {
  const sourceRoot = path.join(repoRoot, "packages", "core", "native");
  if (!fs.existsSync(sourceRoot)) {
    return;
  }
  const targetDirectory = path.join(targetRoot, "native");
  fs.cpSync(sourceRoot, targetDirectory, { recursive: true });
}
