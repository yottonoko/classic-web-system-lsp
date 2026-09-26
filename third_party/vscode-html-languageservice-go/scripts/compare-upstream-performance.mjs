#!/usr/bin/env node
import { existsSync, mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';

const repoRoot = new URL('..', import.meta.url).pathname.replace(/\/$/, '');
const upstreamDir = '/private/tmp/vscode-html-languageservice-upstream-main';
const samples = Number(process.env.HTML_LS_PERF_SAMPLES || '11');
const comparedOperations = new Set([
  'scan',
  'parse',
  'symbols2',
  'folding',
  'links',
  'hover',
  'completion',
  'selectionRanges',
  'rename',
]);
const workspaceOperations = new Set([
  'workspaceParse',
  'workspaceSymbols2',
  'workspaceFolding',
  'workspaceLinks',
  'workspaceSelectionRanges',
]);

main();

function main() {
  ensureUpstream();
  const upstreamSha = command('git', ['rev-parse', 'HEAD'], { cwd: upstreamDir }).trim();
  command('npm', ['ci'], { cwd: upstreamDir, stdio: 'inherit' });
  command('npm', ['run', 'compile'], { cwd: upstreamDir, stdio: 'inherit' });

  const goResults = runGoBench();
  const goSequentialResults = runGoBench(['--gomaxprocs=1']);
  const tsResults = runTypeScriptBench();
  const failures = compareResults(goResults, tsResults);

  console.log(`upstream ${upstreamSha}`);
  console.log(`go gomaxprocs ${goResults[0]?.gomaxprocs ?? 'unknown'} parallel=${goResults[0]?.parallelEnabled ? 'true' : 'false'}`);
  printSummary(goResults, tsResults);
  printWorkspaceSummary(goResults, tsResults);
  printGoParallelSummary(goResults, goSequentialResults);
  if (failures.length > 0) {
    console.error('\nGo was not faster for:');
    for (const failure of failures) {
      console.error(`- ${failure}`);
    }
    process.exit(1);
  }
}

function runGoBench(extraArgs = []) {
  return JSON.parse(command('go', ['run', './internal/perfbench', `--samples=${samples}`, ...extraArgs], { cwd: repoRoot }));
}

function ensureUpstream() {
  if (!existsSync(join(upstreamDir, '.git'))) {
    command('git', ['clone', 'https://github.com/microsoft/vscode-html-languageservice.git', upstreamDir], { stdio: 'inherit' });
  }
  command('git', ['fetch', 'origin', 'main'], { cwd: upstreamDir, stdio: 'inherit' });
  command('git', ['switch', 'main'], { cwd: upstreamDir, stdio: 'inherit' });
  command('git', ['pull', '--ff-only', 'origin', 'main'], { cwd: upstreamDir, stdio: 'inherit' });
}

function runTypeScriptBench() {
  const benchDir = mkdtempSync(join(tmpdir(), 'vscode-html-ls-ts-bench-'));
  const benchPath = join(benchDir, 'bench.mjs');
  writeFileSync(benchPath, tsBenchSource(samples), 'utf8');
  return JSON.parse(command('node', [benchPath], { cwd: upstreamDir }));
}

function compareResults(goResults, tsResults) {
  const tsByKey = new Map(tsResults.map((result) => [resultKey(result), result]));
  const failures = [];
  for (const goResult of goResults) {
    if (!comparedOperations.has(goResult.operation)) {
      continue;
    }
    const tsResult = tsByKey.get(resultKey(goResult));
    if (!tsResult) {
      failures.push(`${goResult.case}/${goResult.operation}: missing TypeScript result`);
      continue;
    }
    if (!(goResult.medianMs < tsResult.medianMs)) {
      failures.push(`${goResult.case}/${goResult.operation}: Go ${goResult.medianMs.toFixed(3)}ms >= TS ${tsResult.medianMs.toFixed(3)}ms`);
    }
  }
  return failures;
}

function printSummary(goResults, tsResults) {
  const tsByKey = new Map(tsResults.map((result) => [resultKey(result), result]));
  for (const goResult of goResults) {
    if (!comparedOperations.has(goResult.operation)) {
      continue;
    }
    const tsResult = tsByKey.get(resultKey(goResult));
    if (!tsResult) {
      continue;
    }
    const speedup = tsResult.medianMs / goResult.medianMs;
    console.log(`${goResult.case.padEnd(13)} ${goResult.operation.padEnd(15)} Go ${goResult.medianMs.toFixed(3).padStart(8)}ms  TS ${tsResult.medianMs.toFixed(3).padStart(8)}ms  ${speedup.toFixed(2)}x`);
  }
}

function printGoParallelSummary(goResults, goSequentialResults) {
  const sequentialByKey = new Map(goSequentialResults.map((result) => [resultKey(result), result]));
  const operations = new Set(['symbols2', 'links', 'selectionRangesBulk', ...workspaceOperations]);
  console.log('\nGo parallel/default vs Go GOMAXPROCS=1:');
  for (const goResult of goResults) {
    if (!operations.has(goResult.operation)) {
      continue;
    }
    const sequential = sequentialByKey.get(resultKey(goResult));
    if (!sequential) {
      continue;
    }
    const speedup = sequential.medianMs / goResult.medianMs;
    console.log(`${goResult.case.padEnd(13)} ${goResult.operation.padEnd(20)} parallel ${goResult.medianMs.toFixed(3).padStart(8)}ms  seq ${sequential.medianMs.toFixed(3).padStart(8)}ms  ${speedup.toFixed(2)}x`);
  }
}

function printWorkspaceSummary(goResults, tsResults) {
  const tsByKey = new Map(tsResults.map((result) => [resultKey(result), result]));
  console.log('\nWorkspace multi-file:');
  for (const goResult of goResults) {
    if (!workspaceOperations.has(goResult.operation)) {
      continue;
    }
    const tsResult = tsByKey.get(resultKey(goResult));
    if (!tsResult) {
      continue;
    }
    const speedup = tsResult.medianMs / goResult.medianMs;
    console.log(`${goResult.case.padEnd(16)} ${goResult.operation.padEnd(25)} Go ${goResult.medianMs.toFixed(3).padStart(8)}ms  TS ${tsResult.medianMs.toFixed(3).padStart(8)}ms  ${speedup.toFixed(2)}x`);
  }
}

function resultKey(result) {
  return `${result.case}/${result.operation}`;
}

function command(commandName, args, options = {}) {
  const result = spawnSync(commandName, args, {
    cwd: options.cwd,
    env: { ...process.env, ...options.env },
    encoding: 'utf8',
    stdio: options.stdio || 'pipe',
  });
  if (result.status !== 0) {
    if (result.stdout) {
      process.stdout.write(result.stdout);
    }
    if (result.stderr) {
      process.stderr.write(result.stderr);
    }
    throw new Error(`${commandName} ${args.join(' ')} failed with exit code ${result.status}`);
  }
  return result.stdout;
}

function tsBenchSource(sampleCount) {
  return `
import { getLanguageService, TokenType } from '${join(upstreamDir, 'lib/esm/htmlLanguageService.js')}';
import { TextDocument } from '${join(upstreamDir, 'node_modules/vscode-languageserver-textdocument/lib/esm/main.js')}';

const samples = ${sampleCount};
const identityContext = { resolveReference(ref, base) { return ref; } };
const ls = getLanguageService();
const results = [];
for (const count of [100, 300, 1000]) {
  const name = 'sections-' + count;
  const text = makeHTML(count);
  const doc = TextDocument.create('file:///bench.html', 'html', 0, text);
  const htmlDoc = ls.parseHTMLDocument(doc);
  const completionPos = doc.positionAt(text.lastIndexOf('<div cl') + '<div cl'.length);
  const hoverPos = doc.positionAt(text.indexOf('<section') + 2);
  const renamePositions = sectionRenamePositions(doc, text);
  const selectionPositions = [
    doc.positionAt(Math.floor(text.length / 4)),
    doc.positionAt(Math.floor(text.length / 2)),
    doc.positionAt(Math.floor(text.length * 3 / 4)),
  ];
  const ops = [
    ['scan', () => {
      const scanner = ls.createScanner(text);
      let count = 0;
      while (true) {
        const token = scanner.scan();
        count += token + scanner.getTokenOffset();
        if (token === TokenType.EOS) break;
      }
      return count;
    }],
    ['parse', () => ls.parseHTMLDocument(doc).roots.length],
    ['symbols2', () => ls.findDocumentSymbols2(doc, htmlDoc).length],
    ['folding', () => ls.getFoldingRanges(doc).length],
    ['links', () => ls.findDocumentLinks(doc, identityContext).length],
    ['hover', () => {
      const hover = ls.doHover(doc, hoverPos, htmlDoc);
      return hover ? hoverContentLength(hover.contents) : 0;
    }],
    ['completion', () => ls.doComplete(doc, completionPos, htmlDoc).items.length],
    ['selectionRanges', () => ls.getSelectionRanges(doc, selectionPositions).length],
    ['rename', () => {
      let count = 0;
      for (const pos of renamePositions) {
        const edit = ls.doRename(doc, pos, 'article', htmlDoc);
        if (edit && edit.changes) {
          count += Object.keys(edit.changes).length;
        }
      }
      return count;
    }],
  ];
  for (const [operation, fn] of ops) {
    const iterations = iterationsFor(operation, text.length);
    const measured = measure(samples, iterations, fn);
    results.push({
      runtime: 'typescript',
      case: name,
      sizeBytes: text.length,
      operation,
      samples,
      iterations,
      medianMs: measured.medianMs,
      minMs: measured.minMs,
      checksum: measured.checksum,
    });
  }
}
{
  const name = 'workspace-24x100';
  const docs = makeWorkspaceDocuments(24, 100);
  const sizeBytes = docs.reduce((sum, entry) => sum + entry.text.length, 0);
  const ops = [
    ['workspaceParse', () => sumInts(docs.map((entry) => ls.parseHTMLDocument(entry.doc).roots.length))],
    ['workspaceSymbols2', () => sumInts(docs.map((entry) => ls.findDocumentSymbols2(entry.doc, entry.htmlDoc).length))],
    ['workspaceFolding', () => sumInts(docs.map((entry) => ls.getFoldingRanges(entry.doc).length))],
    ['workspaceLinks', () => sumInts(docs.map((entry) => ls.findDocumentLinks(entry.doc, identityContext).length))],
    ['workspaceSelectionRanges', () => sumInts(docs.map((entry) => ls.getSelectionRanges(entry.doc, entry.selectionPositions).length))],
  ];
  for (const [operation, fn] of ops) {
    const iterations = iterationsFor(operation, sizeBytes);
    const measured = measure(samples, iterations, fn);
    results.push({
      runtime: 'typescript',
      case: name,
      sizeBytes,
      operation,
      samples,
      iterations,
      medianMs: measured.medianMs,
      minMs: measured.minMs,
      checksum: measured.checksum,
    });
  }
}
console.log(JSON.stringify(results));

function measure(samples, iterations, fn) {
  const values = [];
  let checksum = 0;
  const warmupIterations = Math.max(iterations, 10);
  for (let i = 0; i < warmupIterations; i++) {
    fn();
  }
  for (let sample = 0; sample < samples; sample++) {
    const start = process.hrtime.bigint();
    let local = 0;
    for (let i = 0; i < iterations; i++) {
      local += fn();
    }
    const elapsed = Number(process.hrtime.bigint() - start) / 1e6;
    values.push(elapsed / iterations);
    checksum += local;
  }
  values.sort((a, b) => a - b);
  return { medianMs: values[Math.floor(values.length / 2)], minMs: values[0], checksum };
}

function hoverContentLength(contents) {
  if (typeof contents === 'string') {
    return contents.length;
  }
  if (contents && typeof contents === 'object') {
    return String(contents.kind || '').length + String(contents.value || '').length;
  }
  return String(contents).length;
}

function sectionRenamePositions(doc, text) {
  const positions = [];
  let offset = 0;
  while (offset < text.length) {
    const index = text.indexOf('<section', offset);
    if (index < 0) {
      break;
    }
    positions.push(doc.positionAt(index + '<'.length));
    offset = index + '<section'.length;
  }
  return positions;
}

function makeWorkspaceDocuments(fileCount, sectionsPerFile) {
  const docs = [];
  for (let i = 0; i < fileCount; i++) {
    const text = makeHTML(sectionsPerFile + (i % 3));
    const doc = TextDocument.create(\`file:///workspace/file-\${String(i).padStart(2, '0')}.html\`, 'html', 0, text);
    docs.push({
      text,
      doc,
      htmlDoc: ls.parseHTMLDocument(doc),
      selectionPositions: selectionPositionsForSections(doc, text),
    });
  }
  return docs;
}

function selectionPositionsForSections(doc, text) {
  const positions = [];
  for (const marker of ['<h2>Title ', 'href="/docs/', '<strong>nested']) {
    let offset = 0;
    while (offset < text.length) {
      const index = text.indexOf(marker, offset);
      if (index < 0) {
        break;
      }
      positions.push(doc.positionAt(index + marker.length));
      offset = index + marker.length;
    }
  }
  return positions;
}

function sumInts(values) {
  return values.reduce((sum, value) => sum + value, 0);
}

function iterationsFor(operation, size) {
  switch (operation) {
    case 'workspaceParse':
    case 'workspaceSymbols2':
    case 'workspaceFolding':
    case 'workspaceLinks':
    case 'workspaceSelectionRanges':
      return 3;
    case 'hover':
      return 10000;
    case 'rename':
      return 20;
    case 'parse':
    case 'selectionRanges':
      if (size > 150000) return 20;
      return 50;
    case 'completion':
      return 200;
    default:
      if (size > 500000) return 3;
      if (size > 150000) return 5;
      return 10;
  }
}

function makeHTML(count) {
  let text = '<!doctype html>\\n<html>\\n<head>\\n';
  text += '<title>bench</title><link href="/assets/site.css" rel="stylesheet">\\n';
  text += '<style>.card{display:flex;color:#123}.item{padding:4px}</style>\\n';
  text += '</head>\\n<body>\\n<main id="app">\\n';
  for (let i = 0; i < count; i++) {
    text += \`<section id="s\${i}" class="card item" data-index="\${i}">\\n\`;
    text += \`<h2>Title \${i}</h2><a href="/docs/\${i}.html">link</a>\\n\`;
    text += '<p class="copy">This is a benchmark paragraph with <strong>nested</strong> inline content.</p>\\n';
    text += '<ul><li>alpha</li><li>beta</li><li>gamma</li></ul>\\n';
    text += '</section>\\n';
  }
  text += '<div cl></div>\\n</main>\\n<script src="/assets/app.js"></script>\\n</body>\\n</html>\\n';
  return text;
}
`;
}
