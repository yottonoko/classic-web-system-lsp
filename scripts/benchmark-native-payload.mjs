import { spawn } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { performance } from "node:perf_hooks";

// native worker (--jsonl) を直接駆動し、operation ごとに
// 「レスポンスのバイト数」と「JS 側デコード時間」を分離計測する開発用ハーネス。
// 本番コードには手を入れず、parseAspDocument 高速化の各フェーズの効果を数値で確認する。
//
// 使い方:
//   node scripts/benchmark-native-payload.mjs [sample]
//   sample = large | huge | include-tree (既定 large)

const root = path.resolve(import.meta.dirname, "..");
const executable = process.platform === "win32" ? "asp-lsp-core.exe" : "asp-lsp-core";
const binary = path.join(root, "target", "release", executable);

const sampleConfigs = new Map([
  ["large", "classic-asp-large-benchmark"],
  ["huge", "classic-asp-huge-benchmark"],
  ["include-tree", "classic-asp-include-tree-benchmark"],
]);

const sampleName = process.argv[2] ?? "large";
const sampleDir = sampleConfigs.get(sampleName);
if (!sampleDir) {
  throw new Error(
    `unknown sample: ${sampleName} (expected ${[...sampleConfigs.keys()].join(", ")})`,
  );
}
if (!fs.existsSync(binary)) {
  throw new Error(`native binary missing: ${binary}. Run \`node scripts/build-native-core.mjs\`.`);
}

const sampleRoot = path.join(root, "samples", sampleDir);
const generator = path.join(sampleRoot, "generate.mjs");

class JsonlClient {
  constructor() {
    this.child = spawn(binary, ["--jsonl"], { stdio: "pipe" });
    this.child.stdout.setEncoding("utf8");
    this.buffer = "";
    this.pending = new Map();
    this.nextId = 1;
    this.child.stdout.on("data", (chunk) => this.onData(chunk));
    this.child.stderr.setEncoding("utf8");
    this.stderr = "";
    this.child.stderr.on("data", (chunk) => (this.stderr += chunk));
  }

  onData(chunk) {
    this.buffer += chunk;
    let newline;
    while ((newline = this.buffer.indexOf("\n")) !== -1) {
      const line = this.buffer.slice(0, newline);
      this.buffer = this.buffer.slice(newline + 1);
      if (!line.trim()) {
        continue;
      }
      const bytes = Buffer.byteLength(line);
      // レスポンス行を JSON.parse して id を取り出す前に、バイト数を id 無しで取れないので
      // ここでは行全体を保持し、parse 時間を計測しつつ id を引く。
      const parseStart = performance.now();
      const response = JSON.parse(line);
      const parseMs = performance.now() - parseStart;
      const entry = this.pending.get(response.id);
      if (entry) {
        this.pending.delete(response.id);
        entry.resolve({ response, bytes, parseMs });
      }
    }
  }

  request(req) {
    const id = this.nextId++;
    const payload = `${JSON.stringify({ id, request: req })}\n`;
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      this.child.stdin.write(payload, "utf8", (error) => {
        if (error) {
          this.pending.delete(id);
          reject(error);
        }
      });
    });
  }

  close() {
    this.child.stdin.end();
    this.child.kill();
  }
}

function collectSources() {
  const generated = path.join(sampleRoot, "includes", "generated");
  const relativePaths = [
    "default.asp",
    "includes/layer1.inc",
    "includes/layer2.inc",
    "includes/layer3.inc",
    "includes/layer4.inc",
    ...(fs.existsSync(generated)
      ? fs
          .readdirSync(generated)
          .filter((entry) => entry.endsWith(".inc"))
          .sort()
          .map((entry) => path.join("includes", "generated", entry))
      : []),
  ];
  return relativePaths
    .map((relativePath) => path.join(sampleRoot, relativePath))
    .filter((absolutePath) => fs.existsSync(absolutePath))
    .map((absolutePath) => ({
      uri: `file://${absolutePath}`,
      text: fs.readFileSync(absolutePath, "utf8"),
    }));
}

async function measureOperation(client, operation, sources, buildRequest) {
  let totalBytes = 0;
  let totalParseMs = 0;
  let totalInputBytes = 0;
  for (const source of sources) {
    const { bytes, parseMs, response } = await client.request(buildRequest(source));
    if (!response.ok) {
      throw new Error(`${operation} failed: ${response.error}`);
    }
    totalBytes += bytes;
    totalParseMs += parseMs;
    totalInputBytes += Buffer.byteLength(source.text);
  }
  return { operation, totalBytes, totalParseMs, totalInputBytes };
}

function formatBytes(bytes) {
  if (bytes >= 1024 * 1024) {
    return `${(bytes / 1024 / 1024).toFixed(2)} MB`;
  }
  return `${(bytes / 1024).toFixed(1)} KB`;
}

async function main() {
  if (fs.existsSync(generator)) {
    const { execFileSync } = await import("node:child_process");
    execFileSync(process.execPath, [generator], { stdio: "inherit" });
  }
  const sources = collectSources();
  const inputBytes = sources.reduce((sum, source) => sum + Buffer.byteLength(source.text), 0);
  const client = new JsonlClient();
  try {
    const operations = [
      [
        "parseAspDocument",
        (source) => ({ operation: "parseAspDocument", uri: source.uri, text: source.text }),
      ],
      [
        "parseAspDocumentShallow",
        (source) => ({ operation: "parseAspDocumentShallow", uri: source.uri, text: source.text }),
      ],
      [
        "parseAspDocumentVbscript",
        (source) => ({ operation: "parseAspDocumentVbscript", uri: source.uri, text: source.text }),
      ],
    ];
    const results = [];
    for (const [operation, build] of operations) {
      // warmup（native 側キャッシュを温める）
      await measureOperation(client, operation, sources, build);
      results.push(await measureOperation(client, operation, sources, build));
    }

    console.log("");
    console.log(`Native payload benchmark: ${sampleName}`);
    console.log(`Files: ${sources.length}`);
    console.log(`Input: ${formatBytes(inputBytes)}`);
    console.log("");
    const rows = [
      ["Operation", "payload", "x input", "JSON.parse ms"],
      ...results.map((result) => [
        result.operation,
        formatBytes(result.totalBytes),
        `${(result.totalBytes / result.totalInputBytes).toFixed(1)}x`,
        result.totalParseMs.toFixed(2),
      ]),
    ];
    const widths = rows[0].map((_, column) => Math.max(...rows.map((row) => row[column].length)));
    for (const [index, row] of rows.entries()) {
      console.log(row.map((value, column) => value.padEnd(widths[column])).join("  "));
      if (index === 0) {
        console.log(widths.map((width) => "-".repeat(width)).join("  "));
      }
    }
  } finally {
    client.close();
  }
}

await main();
