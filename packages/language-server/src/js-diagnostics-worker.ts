import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { parentPort } from "node:worker_threads";
import ts from "typescript";
import type {
  JsDiagnosticsWorkerRequest,
  JsDiagnosticsWorkerResponse,
  JsDiagnosticsWorkerTiming,
  JsDiagnosticsWorkerTsDiagnostic,
  JsDiagnosticsWorkerVirtualDocument,
} from "./js-diagnostics-protocol";
import { fileIdentityKeyFromFileName } from "./file-identity";

if (!parentPort) {
  throw new Error("JavaScript diagnostics worker requires a parent port.");
}

const browserJavaScriptLibs = ["lib.esnext.d.ts", "lib.dom.d.ts", "lib.dom.iterable.d.ts"];
const hiddenNodeTypeName = "node";
const fileExistsCache = new Map<string, boolean>();
const readFileCache = new Map<string, string | undefined>();
const directoryExistsCache = new Map<string, boolean>();
const directoriesCache = new Map<string, string[]>();
const readDirectoryCache = new Map<string, string[]>();
const realpathCache = new Map<string, string>();
const fileStatCache = new Map<string, CachedFileStat | undefined>();
const configCache = new Map<string, { config: JsProjectConfig; lastUsed: number }>();
let cacheTick = 0;
let currentProjectGeneration: number | undefined;

interface JsProjectFile {
  fileName: string;
  text: string;
  version: string;
}

interface JsProjectConfig {
  fileNames: string[];
  options: ts.CompilerOptions;
  currentDirectory: string;
}

interface CachedFileStat {
  mtimeMs: number;
  size: number;
}

parentPort.on("message", (request: JsDiagnosticsWorkerRequest) => {
  const timings: JsDiagnosticsWorkerTiming[] = [];
  try {
    resetCachesForProjectGeneration(request.projectGeneration);
    const diagnostics = measure(timings, "javascriptSemantic", () => semanticDiagnostics(request));
    parentPort?.postMessage({
      id: request.id,
      diagnostics,
      timings,
    } satisfies JsDiagnosticsWorkerResponse);
  } catch (error) {
    parentPort?.postMessage({
      id: request.id,
      error: serializeWorkerError(error),
    } satisfies JsDiagnosticsWorkerResponse);
  }
});

function resetCachesForProjectGeneration(projectGeneration: number): void {
  if (currentProjectGeneration === projectGeneration) {
    return;
  }
  currentProjectGeneration = projectGeneration;
  fileExistsCache.clear();
  readFileCache.clear();
  directoryExistsCache.clear();
  directoriesCache.clear();
  readDirectoryCache.clear();
  realpathCache.clear();
  fileStatCache.clear();
  configCache.clear();
}

function semanticDiagnostics(
  request: JsDiagnosticsWorkerRequest,
): JsDiagnosticsWorkerTsDiagnostic[] {
  if (request.settings.checkJs !== true) {
    return [];
  }
  const project = createLanguageServiceProject(request);
  try {
    const fileName = normalizeFileName(jsVirtualFileName(request.activeVirtual.uri));
    return project.service.getSemanticDiagnostics(fileName).map(cacheTsDiagnostic);
  } finally {
    project.service.dispose();
  }
}

function createLanguageServiceProject(request: JsDiagnosticsWorkerRequest): {
  service: ts.LanguageService;
} {
  const activeFile = normalizeFileName(jsVirtualFileName(request.activeVirtual.uri));
  const ownerFile = uriToFileName(virtualSourceUri(request.activeVirtual));
  const config = readProjectConfig(
    ownerFile,
    request.settings,
    request.workspaceRoots,
    request.optionOverrides ?? {},
  );
  const files = collectProjectFiles(request, config);
  const moduleResolutionHost: ts.ModuleResolutionHost = {
    fileExists: (requested) =>
      files.has(normalizeFileName(requested)) || cachedFileExists(requested),
    readFile: (requested) =>
      files.get(normalizeFileName(requested))?.text ?? cachedReadFile(requested),
    directoryExists: cachedDirectoryExists,
    getDirectories: cachedGetDirectories,
    realpath: cachedRealpath,
  };
  const moduleResolutionCache = ts.createModuleResolutionCache(
    config.currentDirectory,
    normalizeFileName,
    config.options,
  );
  const host: ts.LanguageServiceHost = {
    getScriptFileNames: () => [...new Set([activeFile, ...files.keys()])],
    getProjectVersion: () => jsProjectFilesFingerprint(files),
    getScriptVersion: (requested) => files.get(normalizeFileName(requested))?.version ?? "0",
    getScriptSnapshot: (requested) => {
      const text = files.get(normalizeFileName(requested))?.text ?? cachedReadFile(requested);
      return text === undefined ? undefined : ts.ScriptSnapshot.fromString(text);
    },
    getScriptKind: scriptKindForFileName,
    getCurrentDirectory: () => config.currentDirectory,
    getCompilationSettings: () => config.options,
    getDefaultLibFileName: (options) => ts.getDefaultLibFilePath(options),
    fileExists: (requested) =>
      files.has(normalizeFileName(requested)) || cachedFileExists(requested),
    readFile: (requested) =>
      files.get(normalizeFileName(requested))?.text ?? cachedReadFile(requested),
    readDirectory: cachedReadDirectory,
    directoryExists: cachedDirectoryExists,
    getDirectories: cachedGetDirectories,
    realpath: cachedRealpath,
    useCaseSensitiveFileNames: () => ts.sys.useCaseSensitiveFileNames,
    resolveModuleNames: (
      moduleNames,
      containingFile,
      _reusedNames,
      _redirectedReference,
      options,
    ) =>
      moduleNames.map(
        (moduleName) =>
          ts.resolveModuleName(
            moduleName,
            containingFile,
            options,
            moduleResolutionHost,
            moduleResolutionCache,
          ).resolvedModule,
      ),
  };
  return {
    service: ts.createLanguageService(host),
  };
}

function collectProjectFiles(
  request: JsDiagnosticsWorkerRequest,
  config: JsProjectConfig,
): Map<string, JsProjectFile> {
  const files = new Map<string, JsProjectFile>();
  for (const virtual of request.openVirtuals) {
    addVirtualFile(files, virtual);
  }
  addVirtualFile(files, request.activeVirtual);
  for (const fileName of config.fileNames) {
    const normalized = normalizeFileName(fileName);
    if (files.has(normalized) || !cachedFileExists(normalized)) {
      continue;
    }
    const text = cachedReadFile(normalized);
    if (text === undefined) {
      continue;
    }
    const stat = cachedFileStat(normalized);
    files.set(normalized, {
      fileName: normalized,
      text,
      version: stat ? `${stat.mtimeMs}:${stat.size}` : "0",
    });
  }
  return files;
}

function addVirtualFile(
  files: Map<string, JsProjectFile>,
  virtual: JsDiagnosticsWorkerVirtualDocument,
): void {
  const fileName = normalizeFileName(jsVirtualFileName(virtual.uri));
  files.set(fileName, {
    fileName,
    text: virtual.text,
    version: JSON.stringify({
      language: virtual.languageId,
      text: textFingerprint(virtual.text),
    }),
  });
}

function readProjectConfig(
  ownerFile: string,
  settings: JsDiagnosticsWorkerRequest["settings"],
  workspaceRoots: string[],
  optionOverrides: Partial<ts.CompilerOptions>,
): JsProjectConfig {
  const ownerDirectory = path.dirname(ownerFile);
  const configPath =
    settings.javascript?.ignoreProjectConfig === true
      ? undefined
      : (ts.findConfigFile(ownerDirectory, cachedFileExists, "tsconfig.json") ??
        ts.findConfigFile(ownerDirectory, cachedFileExists, "jsconfig.json"));
  const cacheKey = configCacheKey(ownerFile, configPath, settings, optionOverrides);
  const cached = configCache.get(cacheKey);
  if (cached) {
    cached.lastUsed = ++cacheTick;
    return cached.config;
  }
  const defaultOptions: ts.CompilerOptions = {
    allowJs: true,
    checkJs: settings.checkJs ?? false,
    noEmit: true,
    target: ts.ScriptTarget.ESNext,
    module: ts.ModuleKind.CommonJS,
    moduleResolution: ts.ModuleResolutionKind.Node10,
    lib: browserJavaScriptLibs,
  };
  const config = configPath
    ? projectConfigFromConfigFile(configPath, defaultOptions, settings, optionOverrides)
    : defaultProjectConfig(
        ownerDirectory,
        workspaceRoots,
        defaultOptions,
        settings,
        optionOverrides,
      );
  configCache.set(cacheKey, { config, lastUsed: ++cacheTick });
  pruneConfigCache();
  return config;
}

function projectConfigFromConfigFile(
  configPath: string,
  defaultOptions: ts.CompilerOptions,
  settings: JsDiagnosticsWorkerRequest["settings"],
  optionOverrides: Partial<ts.CompilerOptions>,
): JsProjectConfig {
  const config = ts.readConfigFile(configPath, cachedReadFile);
  const parseHost: ts.ParseConfigHost = {
    useCaseSensitiveFileNames: ts.sys.useCaseSensitiveFileNames,
    fileExists: cachedFileExists,
    readFile: cachedReadFile,
    readDirectory: cachedReadDirectory,
  };
  const parsed = ts.parseJsonConfigFileContent(
    config.config ?? {},
    parseHost,
    path.dirname(configPath),
    defaultOptions,
    configPath,
  );
  const currentDirectory = path.dirname(configPath);
  return {
    fileNames: parsed.fileNames,
    options: browserCompilerOptions(parsed.options, currentDirectory, settings, optionOverrides),
    currentDirectory,
  };
}

function defaultProjectConfig(
  ownerDirectory: string,
  workspaceRoots: string[],
  defaultOptions: ts.CompilerOptions,
  settings: JsDiagnosticsWorkerRequest["settings"],
  optionOverrides: Partial<ts.CompilerOptions>,
): JsProjectConfig {
  const roots = workspaceRoots.length > 0 ? workspaceRoots : [ownerDirectory];
  const currentDirectory = roots[0] ?? ownerDirectory;
  return {
    fileNames: [],
    options: browserCompilerOptions(defaultOptions, currentDirectory, settings, optionOverrides),
    currentDirectory,
  };
}

function browserCompilerOptions(
  options: ts.CompilerOptions,
  currentDirectory: string,
  settings: JsDiagnosticsWorkerRequest["settings"],
  optionOverrides: Partial<ts.CompilerOptions>,
): ts.CompilerOptions {
  const next: ts.CompilerOptions = {
    ...options,
    ...optionOverrides,
    allowJs: true,
    noEmit: true,
    noLib: false,
    checkJs: optionOverrides.checkJs ?? settings.checkJs ?? false,
  };
  next.lib = ensureBrowserJavaScriptLibs(next.lib);
  next.types = browserJavaScriptTypes(next, currentDirectory);
  return next;
}

function ensureBrowserJavaScriptLibs(libs: string[] | undefined): string[] {
  const existing = new Set((libs ?? browserJavaScriptLibs).map((lib) => lib.toLowerCase()));
  return [
    ...(libs ?? []),
    ...browserJavaScriptLibs.filter((lib) => !existing.has(lib.toLowerCase())),
  ];
}

function browserJavaScriptTypes(
  options: ts.CompilerOptions,
  currentDirectory: string,
): string[] | undefined {
  if (options.types) {
    return options.types.filter((type) => type.toLowerCase() !== hiddenNodeTypeName);
  }
  const host: ts.ModuleResolutionHost = {
    fileExists: cachedFileExists,
    readFile: cachedReadFile,
    directoryExists: cachedDirectoryExists,
    getDirectories: cachedGetDirectories,
    realpath: cachedRealpath,
    getCurrentDirectory: () => currentDirectory,
  };
  return ts
    .getAutomaticTypeDirectiveNames(options, host)
    .filter((type) => type.toLowerCase() !== hiddenNodeTypeName);
}

function configCacheKey(
  ownerFile: string,
  configPath: string | undefined,
  settings: JsDiagnosticsWorkerRequest["settings"],
  optionOverrides: Partial<ts.CompilerOptions>,
): string {
  const environmentFiles = [configPath, nearestPackageJson(path.dirname(ownerFile))]
    .filter((fileName): fileName is string => Boolean(fileName))
    .map((fileName) => {
      const stat = cachedFileStat(fileName);
      return stat ? `${normalizeFileName(fileName)}:${stat.mtimeMs}:${stat.size}` : fileName;
    });
  return JSON.stringify({
    ownerDirectory: normalizeFileName(path.dirname(ownerFile)),
    configPath: configPath ? normalizeFileName(configPath) : undefined,
    environmentFiles,
    settings: {
      checkJs: settings.checkJs ?? false,
      autoImports: settings.javascript?.autoImports !== false,
      unusedDiagnostics: settings.javascript?.unusedDiagnostics !== false,
      ignoreProjectConfig: settings.javascript?.ignoreProjectConfig === true,
    },
    optionOverrides,
  });
}

function nearestPackageJson(directory: string): string | undefined {
  let current = normalizeFileName(directory);
  while (true) {
    const candidate = path.join(current, "package.json");
    if (cachedFileExists(candidate)) {
      return candidate;
    }
    const parent = path.dirname(current);
    if (parent === current) {
      return undefined;
    }
    current = parent;
  }
}

function cachedFileStat(fileName: string): CachedFileStat | undefined {
  const normalized = normalizeFileName(fileName);
  if (fileStatCache.has(normalized)) {
    return fileStatCache.get(normalized);
  }
  const stat = fs.statSync(normalized, { throwIfNoEntry: false });
  const cached = stat ? { mtimeMs: stat.mtimeMs, size: stat.size } : undefined;
  fileStatCache.set(normalized, cached);
  return cached;
}

function pruneConfigCache(): void {
  while (configCache.size > 16) {
    const oldest = [...configCache.entries()].sort(
      (left, right) => left[1].lastUsed - right[1].lastUsed,
    )[0];
    if (!oldest) {
      return;
    }
    configCache.delete(oldest[0]);
  }
}

function cachedFileExists(fileName: string): boolean {
  const key = safeNormalizeFileName(fileName);
  const cached = fileExistsCache.get(key);
  if (cached !== undefined) {
    return cached;
  }
  const exists = ts.sys.fileExists(fileName);
  fileExistsCache.set(key, exists);
  return exists;
}

function cachedReadFile(fileName: string): string | undefined {
  const key = safeNormalizeFileName(fileName);
  if (readFileCache.has(key)) {
    return readFileCache.get(key);
  }
  const text = ts.sys.readFile(fileName);
  readFileCache.set(key, text);
  return text;
}

function cachedDirectoryExists(directory: string): boolean {
  const key = safeNormalizeFileName(directory);
  const cached = directoryExistsCache.get(key);
  if (cached !== undefined) {
    return cached;
  }
  const exists = ts.sys.directoryExists(directory);
  directoryExistsCache.set(key, exists);
  return exists;
}

function cachedGetDirectories(directory: string): string[] {
  const key = safeNormalizeFileName(directory);
  const cached = directoriesCache.get(key);
  if (cached) {
    return cached;
  }
  const directories = getDirectories(directory);
  directoriesCache.set(key, directories);
  return directories;
}

function cachedReadDirectory(
  rootDir: string,
  extensions?: readonly string[],
  excludes?: readonly string[],
  includes?: readonly string[],
  depth?: number,
): string[] {
  const key = JSON.stringify({
    rootDir: safeNormalizeFileName(rootDir),
    extensions,
    excludes,
    includes,
    depth,
  });
  const cached = readDirectoryCache.get(key);
  if (cached) {
    return cached;
  }
  const entries = readDirectory(rootDir, extensions, excludes, includes, depth);
  readDirectoryCache.set(key, entries);
  return entries;
}

function cachedRealpath(fileName: string): string {
  const key = safeNormalizeFileName(fileName);
  const cached = realpathCache.get(key);
  if (cached) {
    return cached;
  }
  const realpath = ts.sys.realpath?.(fileName) ?? fileName;
  realpathCache.set(key, realpath);
  return realpath;
}

function readDirectory(
  rootDir: string,
  extensions?: readonly string[],
  excludes?: readonly string[],
  includes?: readonly string[],
  depth?: number,
): string[] {
  try {
    return ts.sys.readDirectory(rootDir, extensions, excludes, includes, depth);
  } catch {
    return [];
  }
}

function getDirectories(directory: string): string[] {
  try {
    return ts.sys.getDirectories(directory);
  } catch {
    return [];
  }
}

function safeNormalizeFileName(fileName: string): string {
  try {
    return normalizeFileName(fileName);
  } catch {
    return fileName;
  }
}

function normalizeFileName(fileName: string): string {
  return fileIdentityKeyFromFileName(fileName);
}

function uriToFileName(uri: string): string {
  const fileName = uri.startsWith("file://") ? fileURLToPath(uri) : uri;
  return fileName.replace(/\.(html|css|javascript|vbscript|jscript)\.virtual$/, "");
}

function jsVirtualFileName(uri: string): string {
  const fileName = uri.startsWith("file://") ? fileURLToPath(uri) : uri;
  return fileName.replace(/\.(javascript|jscript)\.virtual$/, ".$1.js");
}

function virtualSourceUri(virtual: JsDiagnosticsWorkerVirtualDocument): string {
  return virtual.uri.replace(`.${virtual.languageId}.virtual`, "");
}

function scriptKindForFileName(fileName: string): ts.ScriptKind {
  const lower = fileName.toLowerCase();
  if (lower.endsWith(".ts") || lower.endsWith(".mts") || lower.endsWith(".cts")) {
    return ts.ScriptKind.TS;
  }
  if (lower.endsWith(".tsx")) {
    return ts.ScriptKind.TSX;
  }
  if (lower.endsWith(".jsx")) {
    return ts.ScriptKind.JSX;
  }
  return ts.ScriptKind.JS;
}

function jsProjectFilesFingerprint(files: Map<string, JsProjectFile>): string {
  return JSON.stringify(
    [...files.values()]
      .map((file) => ({
        fileName: file.fileName,
        version: file.version,
      }))
      .sort((left, right) => left.fileName.localeCompare(right.fileName)),
  );
}

function cacheTsDiagnostic(diagnostic: ts.Diagnostic): JsDiagnosticsWorkerTsDiagnostic {
  return {
    code: diagnostic.code,
    category: diagnostic.category,
    messageText: ts.flattenDiagnosticMessageText(diagnostic.messageText, "\n"),
    start: diagnostic.start,
    length: diagnostic.length,
    reportsUnnecessary: diagnostic.reportsUnnecessary === true,
  };
}

function textFingerprint(text: string): string {
  let hash = 2166136261;
  for (let index = 0; index < text.length; index += 1) {
    hash ^= text.charCodeAt(index);
    hash = Math.imul(hash, 16777619);
  }
  return `${text.length}:${hash >>> 0}`;
}

function measure<T>(timings: JsDiagnosticsWorkerTiming[], name: string, callback: () => T): T {
  const startedAt = process.hrtime.bigint();
  try {
    return callback();
  } finally {
    timings.push({
      name,
      elapsedMs: Number(process.hrtime.bigint() - startedAt) / 1_000_000,
    });
  }
}

function serializeWorkerError(error: unknown): JsDiagnosticsWorkerResponse["error"] {
  if (error instanceof Error) {
    return {
      name: error.name,
      message: error.message,
      stack: error.stack,
    };
  }
  return {
    message: String(error),
  };
}
