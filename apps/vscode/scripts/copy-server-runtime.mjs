import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";

const extensionRoot = path.resolve(import.meta.dirname, "..");
const repoRoot = path.resolve(extensionRoot, "..", "..");
const serverRoot = path.join(extensionRoot, "server", "language-server");
const serverNodeModules = path.join(serverRoot, "node_modules");
const copiedPackages = new Set();
const serverPackageJsonPath = path.join(repoRoot, "packages", "language-server", "package.json");

fs.rmSync(path.join(extensionRoot, "server"), { recursive: true, force: true });
fs.mkdirSync(serverRoot, { recursive: true });

copyLocalPackage("packages/language-server", serverRoot);
copyLocalPackage("packages/core", path.join(serverNodeModules, "@asp-lsp", "core"));

for (const dependency of runtimeDependencies(serverPackageJsonPath)) {
  if (dependency === "@asp-lsp/core") {
    continue;
  }
  copyExternalPackage(dependency, serverPackageJsonPath);
}

function copyLocalPackage(relativeSource, destination) {
  const source = path.join(repoRoot, relativeSource);
  fs.mkdirSync(destination, { recursive: true });
  for (const entry of ["package.json", "dist"]) {
    fs.cpSync(path.join(source, entry), path.join(destination, entry), {
      recursive: true,
      force: true,
      dereference: true,
    });
  }
}

function copyExternalPackage(packageName, parentPackageJsonPath) {
  if (copiedPackages.has(packageName)) {
    return;
  }
  copiedPackages.add(packageName);
  const resolver = createRequire(parentPackageJsonPath);
  const packageJsonPath = resolvePackageJson(packageName, resolver);
  const source = path.dirname(packageJsonPath);
  const destination = path.join(serverNodeModules, ...packageName.split("/"));
  fs.mkdirSync(path.dirname(destination), { recursive: true });
  fs.cpSync(source, destination, {
    recursive: true,
    force: true,
    dereference: true,
    filter: (entry) => !path.relative(source, entry).split(path.sep).includes("node_modules"),
  });
  for (const dependency of runtimeDependencies(packageJsonPath)) {
    copyExternalPackage(dependency, packageJsonPath);
  }
}

function resolvePackageJson(packageName, resolver) {
  try {
    return resolver.resolve(`${packageName}/package.json`);
  } catch {
    let directory = path.dirname(resolver.resolve(packageName));
    while (directory !== path.dirname(directory)) {
      const candidate = path.join(directory, "package.json");
      if (fs.existsSync(candidate)) {
        return candidate;
      }
      directory = path.dirname(directory);
    }
    throw new Error(`Could not resolve package.json for ${packageName}`);
  }
}

function runtimeDependencies(packageJsonPath) {
  const manifest = JSON.parse(fs.readFileSync(packageJsonPath, "utf8"));
  return Object.keys(manifest.dependencies ?? {});
}
