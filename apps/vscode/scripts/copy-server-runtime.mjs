import fs from "node:fs";
import { builtinModules, createRequire } from "node:module";
import os from "node:os";
import path from "node:path";
import { rolldown } from "rolldown";

const extensionRoot = path.resolve(import.meta.dirname, "..");
const repoRoot = path.resolve(extensionRoot, "..", "..");
const sidecarRoot = path.join(extensionRoot, "server", "sidecar");
const sidecarEntry = path.join(repoRoot, "packages", "embedded-sidecar", "dist", "sidecar.js");
const includeRustServer = !process.argv.includes("--no-native");
const nodeBuiltins = new Set([...builtinModules, ...builtinModules.map((name) => `node:${name}`)]);
const require = createRequire(import.meta.url);
const sidecarManifest = JSON.parse(
  fs.readFileSync(path.join(repoRoot, "packages", "embedded-sidecar", "package.json"), "utf8"),
);

if (!fs.existsSync(sidecarEntry)) {
  throw new Error(`Build @asp-lsp/embedded-sidecar before packaging: ${sidecarEntry}`);
}

fs.rmSync(path.join(extensionRoot, "server"), { recursive: true, force: true });
const sidecarDistRoot = path.join(sidecarRoot, "dist");
fs.mkdirSync(sidecarDistRoot, { recursive: true });

await bundleNodeEntry(sidecarEntry, path.join(sidecarDistRoot, "sidecar.js"));
fs.chmodSync(path.join(sidecarDistRoot, "sidecar.js"), 0o755);
copyTypeScriptLibs(sidecarDistRoot);
if (includeRustServer) {
  copyRustServer(extensionRoot);
}
fs.writeFileSync(
  path.join(sidecarRoot, "package.json"),
  `${JSON.stringify(
    {
      name: "@asp-lsp/embedded-sidecar-bundled",
      version: sidecarManifest.version,
      private: true,
      main: "dist/sidecar.js",
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

function copyRustServer(targetRoot) {
  const executable = process.platform === "win32" ? "asp-lsp-server.exe" : "asp-lsp-server";
  const source = path.join(repoRoot, "target", "release", executable);
  if (!fs.existsSync(source)) {
    return;
  }
  const targetDirectory = path.join(targetRoot, "server", "bin", runtimeTarget());
  fs.mkdirSync(targetDirectory, { recursive: true });
  const target = path.join(targetDirectory, executable);
  fs.copyFileSync(source, target);
  fs.chmodSync(target, 0o755);
}

function runtimeTarget() {
  return `${platformName()}-${archName()}`;
}

function platformName() {
  if (process.platform === "win32") {
    return "win32";
  }
  if (process.platform === "darwin") {
    return "darwin";
  }
  if (process.platform === "linux") {
    return "linux";
  }
  return process.platform;
}

function archName() {
  if (process.arch === "x64" || process.arch === "arm64") {
    return process.arch;
  }
  if (os.arch() === "x64" || os.arch() === "arm64") {
    return os.arch();
  }
  return process.arch;
}
