import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const repoRoot = path.resolve(import.meta.dirname, "..");
const target = readTarget();
const executable = process.platform === "win32" ? "asp-lsp-core.exe" : "asp-lsp-core";

execFileSync("cargo", ["build", "--release", "-p", "asp-lsp-core"], {
  cwd: repoRoot,
  stdio: "inherit",
});

const source = path.join(repoRoot, "target", "release", executable);
const outDir = path.join(repoRoot, "packages", "core", "native", target);
fs.mkdirSync(outDir, { recursive: true });
fs.copyFileSync(source, path.join(outDir, executable));

console.log(`native core: ${target} -> ${path.relative(repoRoot, path.join(outDir, executable))}`);

function readTarget() {
  const index = process.argv.indexOf("--target");
  if (index !== -1 && process.argv[index + 1]) {
    return process.argv[index + 1];
  }
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
