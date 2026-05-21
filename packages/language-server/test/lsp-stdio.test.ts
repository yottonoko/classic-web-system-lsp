import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, expect, it } from "vitest";

interface JsonRpcMessage {
  id?: number;
  method?: string;
  params?: unknown;
  result?: unknown;
}

describe("stdio LSP server", () => {
  it("handles initialize, didOpen, diagnostics and completion over JSON-RPC", async () => {
    const server = new RpcServer();
    try {
      await server.start();
      const initialize = await server.request("initialize", {
        processId: process.pid,
        rootUri: `file://${process.cwd()}`,
        capabilities: {},
      });
      expect(JSON.stringify(initialize)).toContain("completionProvider");

      const uri = "file:///tmp/default.asp";
      server.notify("textDocument/didOpen", {
        textDocument: {
          uri,
          languageId: "classic-asp",
          version: 1,
          text: `<% Option Explicit\nResponse.\nResponse.Write missingName\n%>`,
        },
      });

      const diagnostics = await server.waitForNotification("textDocument/publishDiagnostics");
      expect(JSON.stringify(diagnostics.params)).toContain("missingName");

      const completions = await server.request("textDocument/completion", {
        textDocument: { uri },
        position: { line: 1, character: 9 },
      });
      expect(JSON.stringify(completions)).toContain("Write");

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
    }
  });

  it("reports include cycles through publishDiagnostics", async () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-"));
    const owner = path.join(tempDir, "default.asp");
    const include = path.join(tempDir, "loop.inc");
    fs.writeFileSync(owner, '<!-- #include file="loop.inc" -->\n<% Response.Write 1 %>', "utf8");
    fs.writeFileSync(include, '<!-- #include file="default.asp" -->', "utf8");

    const server = new RpcServer();
    try {
      await server.start();
      await server.request("initialize", {
        processId: process.pid,
        rootUri: `file://${tempDir}`,
        capabilities: {},
      });
      server.notify("textDocument/didOpen", {
        textDocument: {
          uri: `file://${owner}`,
          languageId: "classic-asp",
          version: 1,
          text: fs.readFileSync(owner, "utf8"),
        },
      });

      const diagnostics = await server.waitForNotification("textDocument/publishDiagnostics");
      expect(JSON.stringify(diagnostics.params)).toContain("Include cycle detected");

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  });

  it("reports transitive include cycles that do not return to the owner", async () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-"));
    const owner = path.join(tempDir, "default.asp");
    const first = path.join(tempDir, "first.inc");
    const second = path.join(tempDir, "second.inc");
    fs.writeFileSync(owner, '<!-- #include file="first.inc" -->', "utf8");
    fs.writeFileSync(first, '<!-- #include file="second.inc" -->', "utf8");
    fs.writeFileSync(second, '<!-- #include file="first.inc" -->', "utf8");

    const server = new RpcServer();
    try {
      await server.start();
      await server.request("initialize", {
        processId: process.pid,
        rootUri: `file://${tempDir}`,
        capabilities: {},
      });
      server.notify("textDocument/didOpen", {
        textDocument: {
          uri: `file://${owner}`,
          languageId: "classic-asp",
          version: 1,
          text: fs.readFileSync(owner, "utf8"),
        },
      });

      const diagnostics = await server.waitForNotification("textDocument/publishDiagnostics");
      expect(JSON.stringify(diagnostics.params)).toContain("Include cycle detected");

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  });
});

class RpcServer {
  private child: ChildProcessWithoutNullStreams | undefined;
  private nextId = 1;
  private buffer = Buffer.alloc(0);
  private stderr = "";
  private responses = new Map<number, (message: JsonRpcMessage) => void>();
  private notifications = new Map<string, Array<(message: JsonRpcMessage) => void>>();

  async start(): Promise<void> {
    const serverPath = path.join(process.cwd(), "dist", "server.js");
    this.child = spawn(process.execPath, [serverPath, "--stdio"], {
      cwd: process.cwd(),
      stdio: ["pipe", "pipe", "pipe"],
    });
    this.child.stdout.on("data", (chunk: Buffer) => this.read(chunk));
    this.child.stderr.on("data", (chunk: Buffer) => {
      this.stderr += chunk.toString("utf8");
    });
  }

  request(method: string, params: unknown): Promise<unknown> {
    const id = this.nextId;
    this.nextId += 1;
    this.write({ jsonrpc: "2.0", id, method, params });
    return new Promise((resolve, reject) => {
      this.responses.set(id, (message) => resolve(message.result));
      setTimeout(() => reject(new Error(`Timed out waiting for ${method}: ${this.stderr}`)), 3000);
    });
  }

  notify(method: string, params: unknown): void {
    this.write({ jsonrpc: "2.0", method, params });
  }

  waitForNotification(method: string): Promise<JsonRpcMessage> {
    return new Promise((resolve, reject) => {
      const callbacks = this.notifications.get(method) ?? [];
      callbacks.push(resolve);
      this.notifications.set(method, callbacks);
      setTimeout(() => reject(new Error(`Timed out waiting for ${method}: ${this.stderr}`)), 3000);
    });
  }

  stop(): void {
    this.child?.kill();
  }

  private write(message: unknown): void {
    const body = JSON.stringify(message);
    this.child?.stdin.write(`Content-Length: ${Buffer.byteLength(body, "utf8")}\r\n\r\n${body}`);
  }

  private read(chunk: Buffer): void {
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
      const message = JSON.parse(this.buffer.slice(bodyStart, bodyEnd).toString("utf8")) as JsonRpcMessage;
      this.buffer = this.buffer.slice(bodyEnd);
      if (message.id !== undefined) {
        this.responses.get(message.id)?.(message);
        this.responses.delete(message.id);
      } else if (message.method) {
        const callbacks = this.notifications.get(message.method) ?? [];
        callbacks.shift()?.(message);
      }
    }
  }
}
