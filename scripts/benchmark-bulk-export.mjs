import { spawn } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { performance } from "node:perf_hooks";

const root = path.resolve(import.meta.dirname, "..");
const serverPath = path.join(root, "packages", "language-server", "dist", "server.js");
const fileCount = readPositiveInteger("ASP_LSP_BENCH_BULK_FILES", 20_000);
const targetBytes = readPositiveInteger("ASP_LSP_BENCH_BULK_TARGET_MB", 300) * 1024 * 1024;
const oldSpaceMb = readPositiveInteger("ASP_LSP_BENCH_BULK_OLD_SPACE_MB", 768);
const timeoutMs = readPositiveInteger("ASP_LSP_BENCH_BULK_TIMEOUT_MS", 600_000);

async function main() {
  if (!fs.existsSync(serverPath)) {
    throw new Error(
      "packages/language-server/dist/server.js is missing. Run `pnpm --filter @asp-lsp/language-server run build`.",
    );
  }
  const workspace = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-bulk-export-"));
  const aspLspSettings = {
    debug: { output: "verbose" },
    graph: {
      maxDocuments: fileCount + 10,
      maxTextLength: Math.max(targetBytes * 2, targetBytes + 1024),
      workerSymbolExtraction: true,
    },
    workspace: {
      maxIndexFiles: fileCount + 10,
    },
  };
  const server = new RpcServer({
    args: [`--max-old-space-size=${oldSpaceMb}`, serverPath, "--stdio"],
    timeoutMs,
    configuration: aspLspSettings,
  });
  try {
    const sourceStats = writeSyntheticWorkspace(workspace);
    await server.start();
    await server.request("initialize", {
      processId: process.pid,
      rootUri: pathToFileURL(workspace).href,
      capabilities: {},
    });
    server.notify("initialized", {});
    server.notify("workspace/didChangeConfiguration", {
      settings: {
        aspLsp: {
          ...aspLspSettings,
          graph: {
            ...aspLspSettings.graph,
            maxTextLength: Math.max(targetBytes * 2, sourceStats.bytes + 1024),
          },
        },
      },
    });
    await sleep(100);

    const startedAt = performance.now();
    const graph = await server.request("workspace/executeCommand", {
      command: "aspLsp.server.buildGraph",
      arguments: [{ scope: "workspace", maxDocuments: fileCount + 10 }],
    });
    const elapsedMs = performance.now() - startedAt;
    await sleep(100);
    const logText = [...server.logMessages, server.stderr ?? ""].join("\n");

    console.log("Classic ASP bulk graph export benchmark");
    console.log(`Workspace: ${workspace}`);
    console.log(`Files: ${sourceStats.files.toLocaleString("en-US")}`);
    console.log(`Bytes: ${sourceStats.bytes.toLocaleString("en-US")}`);
    console.log(`Node old space: ${oldSpaceMb} MB`);
    console.log(`Elapsed: ${elapsedMs.toFixed(2)} ms`);
    console.log(`Graph nodes: ${graph?.nodes?.length ?? 0}`);
    console.log(`Graph links: ${graph?.links?.length ?? 0}`);
    console.log(`Graph truncated: ${graph?.truncated?.reason ?? "no"}`);
    console.log(`Bulk spill events: ${countMatches(logText, "asp.graph.bulk.spill.write")}`);
    console.log(`Bulk complete events: ${countMatches(logText, "asp.graph.bulk.complete")}`);
  } finally {
    await server.shutdown().catch(() => undefined);
    server.stop();
    fs.rmSync(workspace, { recursive: true, force: true });
  }
}

function writeSyntheticWorkspace(workspace) {
  const shared =
    "<%\nDim sharedValue\nFunction SharedFn()\nSharedFn = sharedValue\nEnd Function\n%>\n";
  fs.writeFileSync(path.join(workspace, "shared.inc"), shared);
  const perFileBytes = Math.max(256, Math.floor((targetBytes - shared.length) / fileCount));
  const filler = "'".padEnd(Math.max(1, perFileBytes - 160), "x");
  let bytes = Buffer.byteLength(shared, "utf8");
  for (let index = 0; index < fileCount; index += 1) {
    const text = [
      '<!--#include file="shared.inc"-->',
      "<%",
      `Dim localValue${index}`,
      `localValue${index} = SharedFn()`,
      filler,
      "%>",
      "",
    ].join("\n");
    fs.writeFileSync(path.join(workspace, `page-${index}.asp`), text);
    bytes += Buffer.byteLength(text, "utf8");
  }
  return { files: fileCount + 1, bytes };
}

class RpcServer {
  constructor(options) {
    this.options = options;
    this.nextId = 1;
    this.buffer = Buffer.alloc(0);
    this.responses = new Map();
    this.logMessages = [];
  }

  async start() {
    this.child = spawn(process.execPath, this.options.args, {
      cwd: root,
      env: process.env,
      stdio: ["pipe", "pipe", "pipe"],
    });
    this.child.stdout.on("data", (chunk) => this.read(chunk));
    this.child.stderr.on("data", (chunk) => {
      this.stderr = `${this.stderr ?? ""}${chunk.toString("utf8")}`;
    });
  }

  request(method, params) {
    const id = this.nextId;
    this.nextId += 1;
    this.write({ jsonrpc: "2.0", id, method, params });
    return new Promise((resolve, reject) => {
      const timer = setTimeout(
        () => reject(new Error(`Timed out waiting for ${method}: ${this.stderr ?? ""}`)),
        this.options.timeoutMs,
      );
      this.responses.set(id, { method, resolve, reject, timer });
    });
  }

  notify(method, params) {
    this.write({ jsonrpc: "2.0", method, params });
  }

  async shutdown() {
    if (!this.child || this.child.killed) {
      return;
    }
    await this.request("shutdown", null);
    this.notify("exit", undefined);
  }

  stop() {
    this.child?.kill();
  }

  write(message) {
    const body = JSON.stringify(message);
    this.child?.stdin.write(`Content-Length: ${Buffer.byteLength(body, "utf8")}\r\n\r\n${body}`);
  }

  read(chunk) {
    this.buffer = Buffer.concat([this.buffer, chunk]);
    while (true) {
      const headerEnd = this.buffer.indexOf("\r\n\r\n");
      if (headerEnd === -1) {
        return;
      }
      const header = this.buffer.slice(0, headerEnd).toString("utf8");
      const length = /Content-Length:\s*(\d+)/i.exec(header)?.[1];
      if (!length) {
        throw new Error(`Missing Content-Length header: ${header}`);
      }
      const bodyStart = headerEnd + 4;
      const bodyEnd = bodyStart + Number(length);
      if (this.buffer.length < bodyEnd) {
        return;
      }
      const message = JSON.parse(this.buffer.slice(bodyStart, bodyEnd).toString("utf8"));
      this.buffer = this.buffer.slice(bodyEnd);
      this.handleMessage(message);
    }
  }

  handleMessage(message) {
    if (message.id !== undefined && message.method) {
      this.handleServerRequest(message);
      return;
    }
    if (message.id !== undefined) {
      const pending = this.responses.get(message.id);
      if (!pending) {
        return;
      }
      clearTimeout(pending.timer);
      this.responses.delete(message.id);
      if (message.error) {
        pending.reject(
          new Error(`Request ${pending.method} failed: ${JSON.stringify(message.error)}`),
        );
      } else {
        pending.resolve(message.result);
      }
      return;
    }
    if (message.method === "window/logMessage") {
      this.logMessages.push(String(message.params?.message ?? ""));
    }
  }

  handleServerRequest(message) {
    if (message.method === "workspace/configuration") {
      const items = Array.isArray(message.params?.items) ? message.params.items : [];
      this.write({
        jsonrpc: "2.0",
        id: message.id,
        result: items.map(() => this.options.configuration ?? null),
      });
      return;
    }
    this.write({ jsonrpc: "2.0", id: message.id, result: null });
  }
}

function readPositiveInteger(name, fallback) {
  const value = Number(process.env[name]);
  return Number.isFinite(value) && value > 0 ? Math.floor(value) : fallback;
}

function countMatches(text, needle) {
  return text.split(needle).length - 1;
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
