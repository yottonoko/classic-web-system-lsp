import { spawn, type ChildProcess } from "node:child_process";
import * as fs from "node:fs";
import * as path from "node:path";
import { WASI } from "node:wasi";

export type LspMethodKind = "Control" | "Heavy" | "Interactive";

const CONTROL_METHODS = new Set<string>([
  "initialize",
  "initialized",
  "shutdown",
  "exit",
  "textDocument/didOpen",
  "textDocument/didChange",
  "textDocument/didClose",
  "textDocument/didSave",
  "workspace/didChangeConfiguration",
  "workspace/didChangeWatchedFiles",
  "workspace/didChangeWorkspaceFolders",
  "$/setTrace",
]);

const HEAVY_METHODS = new Set<string>([
  "textDocument/semanticTokens/full",
  "textDocument/semanticTokens/full/delta",
  "textDocument/semanticTokens/range",
  "textDocument/codeLens",
  "codeLens/resolve",
  "textDocument/inlayHint",
  "inlayHint/resolve",
  "textDocument/documentSymbol",
  "textDocument/references",
  "textDocument/rename",
  "textDocument/prepareRename",
  "textDocument/foldingRange",
  "textDocument/documentLink",
  "documentLink/resolve",
  "textDocument/formatting",
  "textDocument/rangeFormatting",
  "textDocument/diagnostic",
  "workspace/symbol",
  "workspace/diagnostic",
]);

/**
 * Classifies an LSP JSON-RPC method into `Control`, `Heavy`, or `Interactive`.
 */
export function classifyLspMethod(method: string): LspMethodKind {
  if (CONTROL_METHODS.has(method)) {
    return "Control";
  }
  if (HEAVY_METHODS.has(method)) {
    return "Heavy";
  }
  return "Interactive";
}

const HEADER_DELIMITER = Buffer.from("\r\n\r\n", "ascii");

/**
 * Incremental parser for `Content-Length`-framed LSP JSON-RPC byte streams.
 */
export class FrameParser {
  private buffer: Buffer = Buffer.alloc(0);

  /**
   * Appends `chunk` to the internal buffer and returns all complete message bodies.
   */
  push(chunk: Buffer): Buffer[] {
    if (chunk.length > 0) {
      this.buffer =
        this.buffer.length === 0 ? Buffer.from(chunk) : Buffer.concat([this.buffer, chunk]);
    }
    const frames: Buffer[] = [];

    while (this.buffer.length > 0) {
      const headerEnd = this.buffer.indexOf(HEADER_DELIMITER);
      if (headerEnd === -1) {
        break;
      }

      const headerText = this.buffer.subarray(0, headerEnd).toString("ascii");
      const match = /^content-length:\s*(\d+)\s*$/im.exec(headerText);
      if (!match) {
        // Malformed header without Content-Length: discard through delimiter to resync.
        this.buffer = this.buffer.subarray(headerEnd + HEADER_DELIMITER.length);
        continue;
      }

      const contentLength = Number.parseInt(match[1], 10);
      const bodyStart = headerEnd + HEADER_DELIMITER.length;
      const frameEnd = bodyStart + contentLength;
      if (this.buffer.length < frameEnd) {
        break;
      }

      frames.push(Buffer.from(this.buffer.subarray(bodyStart, frameEnd)));
      this.buffer =
        frameEnd >= this.buffer.length
          ? Buffer.alloc(0)
          : Buffer.from(this.buffer.subarray(frameEnd));
    }

    return frames;
  }
}

function formatLspFrame(body: Buffer): Buffer {
  const header = Buffer.from(`Content-Length: ${body.length}\r\n\r\n`, "ascii");
  return Buffer.concat([header, body]);
}

function writeFrameAtomic(stream: NodeJS.WritableStream, body: Buffer): void {
  stream.write(formatLspFrame(body));
}

function buildPreopens(): Record<string, string> {
  const preopens: Record<string, string> = {};
  const rootsEnv = process.env.ASP_LSP_WASI_PREOPENS ?? "";
  for (const entry of rootsEnv.split(path.delimiter)) {
    const trimmed = entry.trim();
    if (trimmed.length > 0 && fs.existsSync(trimmed)) {
      preopens[trimmed] = trimmed;
    }
  }
  if (Object.keys(preopens).length === 0) {
    const cwd = process.cwd();
    preopens[cwd] = cwd;
  }
  if (process.platform !== "win32" && fs.existsSync("/")) {
    preopens["/"] = "/";
  }
  return preopens;
}

async function runSingleWasiWorker(wasmPath: string, extraArgs: string[]): Promise<void> {
  const wasi = new WASI({
    version: "preview1",
    args: ["classic-asp-lsp", ...extraArgs],
    env: process.env,
    preopens: buildPreopens(),
  });

  const wasmBytes = fs.readFileSync(wasmPath);
  const wasmModule = await WebAssembly.compile(wasmBytes);
  const instance = await WebAssembly.instantiate(
    wasmModule,
    wasi.getImportObject() as WebAssembly.Imports,
  );
  wasi.start(instance);
}

interface JsonRpcMessage {
  id?: string | number | null;
  method?: string;
  params?: { id?: string | number | null } | null;
}

type WorkerRole = "interactive" | "heavy";

function spawnWorkerProcess(wasmPath: string, extraArgs: string[]): ChildProcess {
  const entryScript = process.argv[1];
  return spawn(
    process.execPath,
    ["--no-warnings", entryScript, "--worker", wasmPath, ...extraArgs],
    {
      stdio: ["pipe", "pipe", "inherit"],
      env: process.env,
    },
  );
}

function runDualWorkerCoordinator(wasmPath: string, extraArgs: string[]): void {
  const interactive = spawnWorkerProcess(wasmPath, extraArgs);
  const heavy = spawnWorkerProcess(wasmPath, ["--no-publish", ...extraArgs]);

  const clientParser = new FrameParser();
  const interactiveParser = new FrameParser();
  const heavyParser = new FrameParser();

  const controlRequestIds = new Set<string | number>();
  const pendingClientRequests = new Map<string | number, WorkerRole>();
  const pendingServerRequests = new Map<string | number, WorkerRole>();

  const sendToWorker = (role: WorkerRole, body: Buffer): void => {
    const target = role === "interactive" ? interactive : heavy;
    if (target.stdin && !target.stdin.destroyed && target.stdin.writable) {
      writeFrameAtomic(target.stdin, body);
    }
  };

  const handleClientFrame = (body: Buffer): void => {
    let msg: JsonRpcMessage;
    try {
      msg = JSON.parse(body.toString("utf8")) as JsonRpcMessage;
    } catch {
      sendToWorker("interactive", body);
      return;
    }

    if (typeof msg.method === "string") {
      if (msg.method === "$/cancelRequest") {
        const cancelId = msg.params?.id;
        const owner =
          cancelId !== undefined && cancelId !== null
            ? pendingClientRequests.get(cancelId)
            : undefined;
        if (owner === "interactive" || owner === "heavy") {
          sendToWorker(owner, body);
        } else {
          sendToWorker("interactive", body);
          sendToWorker("heavy", body);
        }
        return;
      }

      const kind = classifyLspMethod(msg.method);
      if (kind === "Control") {
        if (msg.id !== undefined && msg.id !== null) {
          controlRequestIds.add(msg.id);
        }
        sendToWorker("interactive", body);
        sendToWorker("heavy", body);
        return;
      }

      const role: WorkerRole = kind === "Heavy" ? "heavy" : "interactive";
      if (msg.id !== undefined && msg.id !== null) {
        pendingClientRequests.set(msg.id, role);
      }
      sendToWorker(role, body);
      return;
    }

    // Response from client to a server->client request
    if (msg.id !== undefined && msg.id !== null) {
      const owner = pendingServerRequests.get(msg.id) ?? "interactive";
      pendingServerRequests.delete(msg.id);
      sendToWorker(owner, body);
      return;
    }

    sendToWorker("interactive", body);
  };

  const handleWorkerFrame = (role: WorkerRole, body: Buffer): void => {
    let msg: JsonRpcMessage;
    try {
      msg = JSON.parse(body.toString("utf8")) as JsonRpcMessage;
    } catch {
      writeFrameAtomic(process.stdout, body);
      return;
    }

    if (role === "heavy") {
      if (msg.method === "textDocument/publishDiagnostics") {
        return;
      }
      if (
        msg.method === undefined &&
        msg.id !== undefined &&
        msg.id !== null &&
        controlRequestIds.has(msg.id)
      ) {
        controlRequestIds.delete(msg.id);
        return;
      }
    }

    if (typeof msg.method === "string" && msg.id !== undefined && msg.id !== null) {
      pendingServerRequests.set(msg.id, role);
    } else if (msg.method === undefined && msg.id !== undefined && msg.id !== null) {
      pendingClientRequests.delete(msg.id);
    }

    writeFrameAtomic(process.stdout, body);
  };

  process.stdin.on("data", (chunk: Buffer) => {
    for (const body of clientParser.push(chunk)) {
      handleClientFrame(body);
    }
  });

  process.stdin.on("end", () => {
    interactive.stdin?.end();
    heavy.stdin?.end();
  });

  interactive.stdout?.on("data", (chunk: Buffer) => {
    for (const body of interactiveParser.push(chunk)) {
      handleWorkerFrame("interactive", body);
    }
  });

  heavy.stdout?.on("data", (chunk: Buffer) => {
    for (const body of heavyParser.push(chunk)) {
      handleWorkerFrame("heavy", body);
    }
  });

  interactive.on("exit", (code, signal) => {
    if (!heavy.killed) {
      heavy.kill();
    }
    if (signal) {
      process.exit(1);
    }
    process.exit(code ?? 0);
  });

  heavy.on("exit", (code, signal) => {
    if (code !== 0 && code !== null) {
      process.stderr.write(`classic-asp-lsp heavy worker exited with code ${code}\n`);
    } else if (signal) {
      process.stderr.write(`classic-asp-lsp heavy worker exited on signal ${signal}\n`);
    }
  });
}

/**
 * Zero-dependency Node.js WASI runner for `classic-asp-lsp.wasm` (Any OS).
 * Usage:
 *   node dist/wasm-runner.js <classic-asp-lsp.wasm> [args...]
 *   node dist/wasm-runner.js --worker <classic-asp-lsp.wasm> [args...]
 */
async function main(): Promise<void> {
  const args = process.argv.slice(2);
  if (args[0] === "--worker") {
    const [, wasmPath, ...extraArgs] = args;
    if (!wasmPath) {
      process.stderr.write("Usage: wasm-runner.js --worker <classic-asp-lsp.wasm> [args...]\n");
      process.exit(2);
    }
    await runSingleWasiWorker(wasmPath, extraArgs);
    return;
  }

  const [wasmPath, ...extraArgs] = args;
  if (!wasmPath) {
    process.stderr.write("Usage: wasm-runner.js <classic-asp-lsp.wasm> [args...]\n");
    process.exit(2);
  }
  runDualWorkerCoordinator(wasmPath, extraArgs);
}

const isMain =
  typeof require !== "undefined" && typeof module !== "undefined" && require.main === module;

if (isMain) {
  main().catch((err: unknown) => {
    const detail = err instanceof Error && err.stack ? err.stack : String(err);
    process.stderr.write(`classic-asp-lsp wasm runner error: ${detail}\n`);
    process.exit(1);
  });
}
