import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

const extensionRoot = path.resolve(import.meta.dirname, "..");
const repoRoot = path.resolve(extensionRoot, "..", "..");
const targetGOOS = process.env.ASP_LSP_SERVER_GOOS || process.env.GOOS || process.platform;
const targetGOARCH = process.env.ASP_LSP_SERVER_GOARCH || process.env.GOARCH || process.arch;
const executableName =
  targetGOOS === "windows" || targetGOOS === "win32" ? "asp-lsp-go.exe" : "asp-lsp-go";
const sourceBinary = path.join(repoRoot, "bin", executableName);
const targetRoot = path.join(extensionRoot, "server");
const targetBinary = path.join(targetRoot, executableName);
const goBuildArgs = [
  "build",
  "-trimpath",
  "-buildvcs=false",
  "-ldflags=-s -w -buildid=",
  "-o",
  sourceBinary,
  "./cmd/asp-lsp-go",
];

fs.mkdirSync(path.dirname(sourceBinary), { recursive: true });
execFileSync("go", goBuildArgs, {
  cwd: repoRoot,
  env: {
    ...process.env,
    CGO_ENABLED: "0",
    GOOS: targetGOOS === "win32" ? "windows" : targetGOOS,
    GOARCH: targetGOARCH === "x64" ? "amd64" : targetGOARCH,
  },
  stdio: "inherit",
});

fs.rmSync(targetRoot, { recursive: true, force: true });
fs.mkdirSync(targetRoot, { recursive: true });
fs.copyFileSync(sourceBinary, targetBinary);
fs.chmodSync(targetBinary, 0o755);
