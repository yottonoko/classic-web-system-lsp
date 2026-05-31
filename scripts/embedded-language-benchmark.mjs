import { execFileSync } from "node:child_process";
import { createRequire } from "node:module";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { performance } from "node:perf_hooks";
import { benchmarkSourcesForRun, readBenchmarkCacheMode } from "./benchmark-cache-mode.mjs";

const root = path.resolve(import.meta.dirname, "..");
const coreDist = path.join(root, "packages", "core", "dist", "index.js");
const languageServerRequire = createRequire(
  path.join(root, "packages", "language-server", "package.json"),
);
const { getCSSLanguageService } = languageServerRequire("vscode-css-languageservice");
const { getLanguageService: getHtmlLanguageService, TokenType } = languageServerRequire(
  "vscode-html-languageservice",
);
const { TextDocument } = languageServerRequire("vscode-languageserver-textdocument");
const ts = languageServerRequire("typescript");

const htmlService = getHtmlLanguageService();
const cssService = getCSSLanguageService();
const tsUnusedDiagnosticCodes = new Set([6133, 6138, 6192, 6196, 6198]);
const virtualDocumentCache = new Map();
const textDocumentCache = new Map();
const cssStylesheetCache = new Map();
const jsLanguageServiceCache = new Map();
const jsSourceFileCache = new Map();
const textFingerprintCache = new Map();
const textFingerprintCacheMaxEntries = 512;
const parsedTextFingerprints = new WeakMap();
const virtualTextFingerprints = new WeakMap();

export const embeddedOperationNames = [
  "htmlVirtualDocument",
  "cssVirtualDocument",
  "javascriptVirtualDocument",
  "htmlDiagnostics",
  "cssDiagnostics",
  "javascriptSyntaxDiagnostics",
  "javascriptSemanticDiagnostics",
  "javascriptUnusedDiagnostics",
  "javascriptDiagnostics",
];

export function clearEmbeddedBenchmarkCaches() {
  virtualDocumentCache.clear();
  textDocumentCache.clear();
  cssStylesheetCache.clear();
  jsLanguageServiceCache.clear();
  jsSourceFileCache.clear();
  textFingerprintCache.clear();
}

const sampleConfigs = new Map([
  ["large", { directory: "classic-asp-large-benchmark", recursive: false }],
  ["huge", { directory: "classic-asp-huge-benchmark", recursive: false }],
  ["include-tree", { directory: "classic-asp-include-tree-benchmark", recursive: true }],
]);

export async function runEmbeddedOperation(operation, source, core) {
  const parsed = await core.parseAspDocumentAsync(source.uri, source.text);
  parsedTextFingerprints.set(parsed, source.fingerprint ?? textFingerprint(source.text));
  return runEmbeddedOperationForParsed(operation, parsed, core);
}

export function runEmbeddedOperationForParsed(operation, parsed, core) {
  switch (operation) {
    case "htmlVirtualDocument":
      return buildEmbeddedVirtualDocument(parsed, "html", core)?.text.length ?? 0;
    case "cssVirtualDocument":
      return buildEmbeddedVirtualDocument(parsed, "css", core)?.text.length ?? 0;
    case "javascriptVirtualDocument":
      return buildEmbeddedVirtualDocument(parsed, "javascript", core)?.text.length ?? 0;
    case "htmlDiagnostics":
      return htmlDiagnostics(parsed, core).length;
    case "cssDiagnostics":
      return cssDiagnostics(parsed, core).length;
    case "javascriptSyntaxDiagnostics":
      return javascriptSyntaxDiagnostics(parsed, core).length;
    case "javascriptSemanticDiagnostics":
      return javascriptSemanticDiagnostics(parsed, core).length;
    case "javascriptUnusedDiagnostics":
      return javascriptUnusedDiagnostics(parsed, core).length;
    case "javascriptDiagnostics":
      return javascriptDiagnostics(parsed, core).length;
    default:
      throw new Error(`Unknown embedded benchmark operation: ${operation}`);
  }
}

export function collectBenchmarkSources(sampleRoot, sample) {
  const config = sampleConfigs.get(sample);
  if (!config) {
    throw new Error(`Unknown ASP_LSP_BENCH_SAMPLE: ${sample}`);
  }
  const relativePaths = config.recursive
    ? collectRecursiveRelativePaths(sampleRoot)
    : collectFlatRelativePaths(sampleRoot);
  return relativePaths.map((relativePath) => {
    const absolutePath = path.join(sampleRoot, relativePath);
    const text = fs.readFileSync(absolutePath, "utf8");
    return {
      relativePath,
      uri: pathToFileURL(absolutePath).href,
      text,
      fingerprint: textFingerprint(text),
      lines: text.split("\n").length - 1,
      bytes: Buffer.byteLength(text),
    };
  });
}

export function summarizeSources(items) {
  return items.reduce(
    (stats, item) => {
      stats.files += 1;
      stats.lines += item.lines ?? item.text.split("\n").length - 1;
      stats.bytes += item.bytes ?? Buffer.byteLength(item.text);
      return stats;
    },
    { files: 0, lines: 0, bytes: 0 },
  );
}

function buildEmbeddedVirtualDocument(parsed, language, core) {
  const cacheKey = embeddedVirtualCacheKey(parsed, language);
  const cached = virtualDocumentCache.get(cacheKey);
  if (cached !== undefined) {
    return cached ?? undefined;
  }
  const regions = parsed.regions.filter((region) => region.language === language);
  if (regions.length === 0 && language !== "html") {
    virtualDocumentCache.set(cacheKey, null);
    return undefined;
  }
  const virtual = core.buildVirtualDocument(
    parsed.uri,
    parsed.text,
    language,
    regions,
    parsed.regions,
  );
  virtualDocumentCache.set(cacheKey, virtual);
  return virtual;
}

function embeddedVirtualCacheKey(parsed, language) {
  return `${parsed.uri}|${language}|${parsedTextFingerprint(parsed)}`;
}

function htmlDiagnostics(parsed, core) {
  const virtual = buildEmbeddedVirtualDocument(parsed, "html", core);
  if (!virtual) {
    return [];
  }
  const virtualDoc = toTextDocument(virtual);
  const sourceDoc = toSourceTextDocument(virtual, parsed.text);
  const scanner = htmlService.createScanner(virtual.text);
  const diagnostics = [];
  let token = scanner.scan();
  while (token !== TokenType.EOS) {
    const error = scanner.getTokenError();
    if (error) {
      const range = remapVirtualOffsets(
        virtual,
        virtualDoc,
        sourceDoc,
        scanner.getTokenOffset(),
        scanner.getTokenEnd(),
      );
      if (range) {
        diagnostics.push({
          range,
          message: error,
          source: "asp-lsp-html",
        });
      }
    }
    token = scanner.scan();
  }
  return diagnostics;
}

function cssDiagnostics(parsed, core) {
  const virtual = buildEmbeddedVirtualDocument(parsed, "css", core);
  if (!virtual) {
    return [];
  }
  const doc = toTextDocument(virtual);
  const sourceDoc = toSourceTextDocument(virtual, parsed.text);
  return cssService
    .doValidation(doc, cssStylesheet(doc))
    .map((diagnostic) => remapDiagnostic(virtual, doc, sourceDoc, diagnostic))
    .filter(Boolean);
}

function javascriptDiagnostics(parsed, core) {
  const syntax = javascriptSyntaxDiagnostics(parsed, core);
  const semantic = javascriptSemanticDiagnostics(parsed, core);
  const unused = javascriptUnusedDiagnostics(parsed, core);
  const semanticKeys = new Set(semantic.map(diagnosticKey));
  return [
    ...syntax,
    ...semantic,
    ...unused.filter((diagnostic) => !semanticKeys.has(diagnosticKey(diagnostic))),
  ];
}

function javascriptSyntaxDiagnostics(parsed, core) {
  const virtual = buildEmbeddedVirtualDocument(parsed, "javascript", core);
  if (!virtual) {
    return [];
  }
  const sourceFile = jsSourceFile(virtual);
  const parseDiagnostics = sourceFile.parseDiagnostics ?? [];
  return parseDiagnostics
    .map((diagnostic) => remapTsDiagnostic(virtual, parsed.text, diagnostic))
    .filter(Boolean);
}

function javascriptSemanticDiagnostics(parsed, core) {
  const virtual = buildEmbeddedVirtualDocument(parsed, "javascript", core);
  if (!virtual) {
    return [];
  }
  return withJsLanguageService(virtual, parsed.text, false, (service, fileName) =>
    service
      .getSemanticDiagnostics(fileName)
      .map((diagnostic) => remapTsDiagnostic(virtual, parsed.text, diagnostic))
      .filter(Boolean),
  );
}

function javascriptUnusedDiagnostics(parsed, core) {
  const virtual = buildEmbeddedVirtualDocument(parsed, "javascript", core);
  if (!virtual) {
    return [];
  }
  return withJsLanguageService(virtual, parsed.text, true, (service, fileName) =>
    service
      .getSemanticDiagnostics(fileName)
      .filter((diagnostic) => tsUnusedDiagnosticCodes.has(diagnostic.code))
      .map((diagnostic) => remapTsDiagnostic(virtual, parsed.text, diagnostic))
      .filter(Boolean),
  );
}

function withJsLanguageService(virtual, sourceText, unusedOnly, callback) {
  const fileName = normalizeFileName(jsVirtualFileName(virtual.uri));
  const cacheKey = `${unusedOnly ? "unused" : "semantic"}|${fileName}|${virtualTextFingerprint(virtual)}`;
  const cached = jsLanguageServiceCache.get(cacheKey);
  if (cached) {
    return callback(cached.service, fileName, sourceText);
  }
  const files = new Map([[fileName, virtual.text]]);
  const host = {
    getScriptFileNames: () => [fileName],
    getScriptVersion: () => "0",
    getScriptSnapshot: (requested) => {
      const normalized = normalizeFileName(requested);
      const text = files.get(normalized) ?? (unusedOnly ? undefined : ts.sys.readFile(requested));
      return text === undefined ? undefined : ts.ScriptSnapshot.fromString(text);
    },
    getScriptKind: () => ts.ScriptKind.JS,
    getCurrentDirectory: () => path.dirname(uriToFileName(virtualSourceUri(virtual))),
    getCompilationSettings: () => ({
      allowJs: true,
      checkJs: true,
      noEmit: true,
      noLib: unusedOnly,
      noUnusedLocals: unusedOnly,
      noUnusedParameters: unusedOnly,
      target: ts.ScriptTarget.ESNext,
      module: ts.ModuleKind.CommonJS,
      types: [],
    }),
    getDefaultLibFileName: (options) => (unusedOnly ? "" : ts.getDefaultLibFilePath(options)),
    fileExists: (requested) =>
      files.has(normalizeFileName(requested)) || (!unusedOnly && ts.sys.fileExists(requested)),
    readFile: (requested) =>
      files.get(normalizeFileName(requested)) ??
      (unusedOnly ? undefined : ts.sys.readFile(requested)),
    readDirectory: unusedOnly ? () => [] : ts.sys.readDirectory,
    directoryExists: unusedOnly ? () => true : ts.sys.directoryExists,
    getDirectories: unusedOnly ? () => [] : ts.sys.getDirectories,
    useCaseSensitiveFileNames: () => ts.sys.useCaseSensitiveFileNames,
  };
  const service = ts.createLanguageService(host);
  jsLanguageServiceCache.set(cacheKey, { service });
  return callback(service, fileName, sourceText);
}

function cssStylesheet(doc) {
  const cacheKey = `${doc.uri}|${textDocumentFingerprint(doc)}`;
  let stylesheet = cssStylesheetCache.get(cacheKey);
  if (!stylesheet) {
    stylesheet = cssService.parseStylesheet(doc);
    cssStylesheetCache.set(cacheKey, stylesheet);
  }
  return stylesheet;
}

function jsSourceFile(virtual) {
  const fileName = jsVirtualFileName(virtual.uri);
  const cacheKey = `${fileName}|${virtualTextFingerprint(virtual)}`;
  let sourceFile = jsSourceFileCache.get(cacheKey);
  if (!sourceFile) {
    sourceFile = ts.createSourceFile(
      fileName,
      virtual.text,
      ts.ScriptTarget.ESNext,
      true,
      ts.ScriptKind.JS,
    );
    jsSourceFileCache.set(cacheKey, sourceFile);
  }
  return sourceFile;
}

function remapDiagnostic(virtual, virtualDoc, sourceDoc, diagnostic) {
  const start = virtualDoc.offsetAt(diagnostic.range.start);
  const end = virtualDoc.offsetAt(diagnostic.range.end);
  const range = remapVirtualOffsets(virtual, virtualDoc, sourceDoc, start, end);
  return range ? { ...diagnostic, range } : undefined;
}

function remapTsDiagnostic(virtual, sourceText, diagnostic) {
  if (diagnostic.start === undefined || diagnostic.length === undefined) {
    return undefined;
  }
  const virtualDoc = toTextDocument(virtual);
  const sourceDoc = toSourceTextDocument(virtual, sourceText);
  const range = remapVirtualOffsets(
    virtual,
    virtualDoc,
    sourceDoc,
    diagnostic.start,
    diagnostic.start + diagnostic.length,
  );
  if (!range) {
    return undefined;
  }
  return {
    range,
    message: ts.flattenDiagnosticMessageText(diagnostic.messageText, "\n"),
    code: diagnostic.code,
    source: "asp-lsp-typescript",
  };
}

function remapVirtualOffsets(virtual, virtualDoc, sourceDoc, start, end) {
  if (!virtualRangeStaysWithinSegment(virtual, start, end)) {
    return undefined;
  }
  const sourceStart = virtual.sourceMap.toSourceOffset(start);
  const sourceEnd = virtual.sourceMap.toSourceOffset(end);
  if (sourceStart === undefined || sourceEnd === undefined) {
    return undefined;
  }
  return {
    start: sourceDoc.positionAt(sourceStart),
    end: sourceDoc.positionAt(sourceEnd),
  };
}

function virtualRangeStaysWithinSegment(virtual, start, end) {
  const lastOffset = Math.max(start, end - 1);
  const segment = sourceMapSegmentAtVirtualOffset(virtual, start);
  return Boolean(segment && lastOffset < segment.virtualEnd);
}

function sourceMapSegmentAtVirtualOffset(virtual, virtualOffset) {
  return virtual.sourceMap.segments.find(
    (segment) => virtualOffset >= segment.virtualStart && virtualOffset < segment.virtualEnd,
  );
}

function diagnosticKey(diagnostic) {
  const range = diagnostic.range;
  return [
    diagnostic.code ?? "",
    range?.start?.line ?? -1,
    range?.start?.character ?? -1,
    range?.end?.line ?? -1,
    range?.end?.character ?? -1,
  ].join(":");
}

function toTextDocument(virtual) {
  const cacheKey = `${virtual.uri}|${virtual.languageId}|${virtualTextFingerprint(virtual)}`;
  let document = textDocumentCache.get(cacheKey);
  if (!document) {
    document = TextDocument.create(virtual.uri, virtual.languageId, 0, virtual.text);
    document.__aspLspBenchmarkFingerprint = virtualTextFingerprint(virtual);
    textDocumentCache.set(cacheKey, document);
  }
  return document;
}

function toSourceTextDocument(virtual, sourceText) {
  const uri = virtualSourceUri(virtual);
  const cacheKey = `${uri}|classic-asp|${textFingerprint(sourceText)}`;
  let document = textDocumentCache.get(cacheKey);
  if (!document) {
    document = TextDocument.create(uri, "classic-asp", 0, sourceText);
    document.__aspLspBenchmarkFingerprint = textFingerprint(sourceText);
    textDocumentCache.set(cacheKey, document);
  }
  return document;
}

function parsedTextFingerprint(parsed) {
  let fingerprint = parsedTextFingerprints.get(parsed);
  if (!fingerprint) {
    fingerprint = textFingerprint(parsed.text);
    parsedTextFingerprints.set(parsed, fingerprint);
  }
  return fingerprint;
}

function virtualTextFingerprint(virtual) {
  let fingerprint = virtualTextFingerprints.get(virtual);
  if (!fingerprint) {
    fingerprint = textFingerprint(virtual.text);
    virtualTextFingerprints.set(virtual, fingerprint);
  }
  return fingerprint;
}

function textDocumentFingerprint(document) {
  return document.__aspLspBenchmarkFingerprint ?? textFingerprint(document.getText());
}

function textFingerprint(text) {
  const cached = textFingerprintCache.get(text);
  if (cached) {
    textFingerprintCache.delete(text);
    textFingerprintCache.set(text, cached);
    return cached;
  }
  let hash = 2166136261;
  for (let index = 0; index < text.length; index += 1) {
    hash ^= text.charCodeAt(index);
    hash = Math.imul(hash, 16777619);
  }
  const fingerprint = `${text.length}:${(hash >>> 0).toString(16)}`;
  textFingerprintCache.set(text, fingerprint);
  while (textFingerprintCache.size > textFingerprintCacheMaxEntries) {
    const oldest = textFingerprintCache.keys().next().value;
    if (oldest === undefined) {
      break;
    }
    textFingerprintCache.delete(oldest);
  }
  return fingerprint;
}

function virtualSourceUri(virtual) {
  return virtual.uri.replace(`.${virtual.languageId}.virtual`, "");
}

function uriToFileName(uri) {
  const fileName = uri.startsWith("file://") ? fileURLToPath(uri) : uri;
  return fileName.replace(/\.(html|css|javascript|vbscript|jscript)\.virtual$/, "");
}

function jsVirtualFileName(uri) {
  const fileName = uri.startsWith("file://") ? fileURLToPath(uri) : uri;
  return fileName.replace(/\.(javascript|jscript)\.virtual$/, ".$1.js");
}

function normalizeFileName(fileName) {
  return path.resolve(fileName);
}

function collectFlatRelativePaths(sampleRoot) {
  return [
    "default.asp",
    "includes/layer1.inc",
    "includes/layer2.inc",
    "includes/layer3.inc",
    "includes/layer4.inc",
    ...fs
      .readdirSync(path.join(sampleRoot, "includes", "generated"))
      .filter((entry) => entry.endsWith(".inc"))
      .sort()
      .map((entry) => path.join("includes", "generated", entry)),
  ];
}

function collectRecursiveRelativePaths(sampleRoot) {
  const relativePaths = ["default.asp"];
  const stack = [path.join(sampleRoot, "includes")];
  while (stack.length > 0) {
    const directory = stack.pop();
    if (!directory || !fs.existsSync(directory)) {
      continue;
    }
    for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
      const absolutePath = path.join(directory, entry.name);
      if (entry.isDirectory()) {
        stack.push(absolutePath);
      } else if (entry.isFile() && entry.name.endsWith(".inc")) {
        relativePaths.push(path.relative(sampleRoot, absolutePath));
      }
    }
  }
  return relativePaths.sort();
}

async function main() {
  if (!fs.existsSync(coreDist)) {
    throw new Error(
      "packages/core/dist/index.js is missing. Run `pnpm --filter @asp-lsp/core run build`.",
    );
  }
  const sample = readSample();
  const config = sampleConfigs.get(sample);
  if (!config) {
    throw new Error(`ASP_LSP_BENCH_SAMPLE must be one of: ${[...sampleConfigs.keys()].join(", ")}`);
  }
  const sampleRoot = path.join(root, "samples", config.directory);
  const generator = path.join(sampleRoot, "generate.mjs");
  execFileSync(process.execPath, [generator], { stdio: "inherit" });
  const core = createRequire(import.meta.url)(coreDist);
  const sources = collectBenchmarkSources(sampleRoot, sample);
  const sourceStats = summarizeSources(sources);
  const benchmarkIterations = readPositiveInteger("ASP_LSP_BENCH_ITERATIONS", 5);
  const warmupIterations = readNonNegativeInteger("ASP_LSP_BENCH_WARMUPS", 1);
  const benchmarkCacheMode = readBenchmarkCacheMode();
  const benchmarkConcurrency = readPositiveInteger("ASP_LSP_BENCH_CONCURRENCY", 4);
  const results = [];

  for (const operation of embeddedOperationNames) {
    await runBenchmark(results, operation, benchmarkIterations, warmupIterations, async (run) =>
      measureAcrossSources(
        benchmarkSourcesForRun(sources, benchmarkCacheMode, operation, run),
        benchmarkConcurrency,
        async (source) => {
          await runEmbeddedOperation(operation, source, core);
        },
      ),
    );
  }

  console.log("");
  console.log(`Embedded language benchmark`);
  console.log(`Sample: ${sample}`);
  console.log(`Files: ${sourceStats.files}`);
  console.log(`Lines: ${sourceStats.lines.toLocaleString("en-US")}`);
  console.log(`Bytes: ${sourceStats.bytes.toLocaleString("en-US")}`);
  console.log(`Cache mode: ${benchmarkCacheMode}`);
  console.log(`Warmups: ${warmupIterations}`);
  console.log(`Iterations: ${benchmarkIterations}`);
  console.log(`Concurrency: ${benchmarkConcurrency}`);
  console.log("");
  printTable(results);
}

async function measureAcrossSources(sources, concurrency, callback) {
  const start = performance.now();
  await runBounded(sources, concurrency, callback);
  return performance.now() - start;
}

async function runBounded(items, concurrency, callback) {
  let next = 0;
  const workers = Array.from({ length: Math.min(concurrency, items.length) }, async () => {
    for (;;) {
      const index = next;
      next += 1;
      if (index >= items.length) {
        return;
      }
      await callback(items[index]);
    }
  });
  await Promise.all(workers);
}

async function runBenchmark(results, name, iterations, warmups, fn) {
  for (let index = 0; index < warmups; index += 1) {
    await fn({ phase: "warmup", index });
  }
  const samples = [];
  for (let index = 0; index < iterations; index += 1) {
    samples.push(await fn({ phase: "measure", index }));
  }
  samples.sort((left, right) => left - right);
  const total = samples.reduce((sum, value) => sum + value, 0);
  results.push({
    name,
    min: samples[0],
    median: samples[Math.floor(samples.length / 2)],
    mean: total / samples.length,
    max: samples[samples.length - 1],
  });
}

function printTable(items) {
  const rows = [
    ["Operation", "min ms", "median ms", "mean ms", "max ms"],
    ...items.map((item) => [
      item.name,
      formatMillis(item.min),
      formatMillis(item.median),
      formatMillis(item.mean),
      formatMillis(item.max),
    ]),
  ];
  const widths = rows[0].map((_, column) => Math.max(...rows.map((row) => row[column].length)));
  for (const [index, row] of rows.entries()) {
    console.log(row.map((value, column) => value.padEnd(widths[column])).join("  "));
    if (index === 0) {
      console.log(widths.map((width) => "-".repeat(width)).join("  "));
    }
  }
}

function formatMillis(value) {
  return value.toFixed(2);
}

function readSample() {
  return process.env.ASP_LSP_BENCH_SAMPLE || "large";
}

function readPositiveInteger(name, fallback) {
  const raw = process.env[name];
  if (raw === undefined || raw === "") {
    return fallback;
  }
  const value = Number(raw);
  if (!Number.isInteger(value) || value <= 0) {
    throw new Error(`${name} must be a positive integer.`);
  }
  return value;
}

function readNonNegativeInteger(name, fallback) {
  const raw = process.env[name];
  if (raw === undefined || raw === "") {
    return fallback;
  }
  const value = Number(raw);
  if (!Number.isInteger(value) || value < 0) {
    throw new Error(`${name} must be a non-negative integer.`);
  }
  return value;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  await main();
}
