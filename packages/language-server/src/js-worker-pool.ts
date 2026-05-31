import os from "node:os";
import path from "node:path";
import { Worker } from "node:worker_threads";
import type {
  JsDiagnosticsWorkerRequest,
  JsDiagnosticsWorkerResponse,
} from "./js-diagnostics-protocol";

export interface JsDiagnosticsWorkerCancellation {
  isCancellationRequested(): boolean;
}

interface WorkerSlot {
  worker: Worker;
  task?: WorkerTask;
}

interface WorkerTask {
  request: JsDiagnosticsWorkerRequest;
  enqueuedAt: bigint;
  payloadBytes: number;
  isCancellationRequested?: () => boolean;
  resolve(response: JsDiagnosticsWorkerResponse): void;
  reject(error: Error): void;
}

export class JsDiagnosticsWorkerPool {
  private readonly workerPath: string;
  private readonly queue: WorkerTask[] = [];
  private readonly slots: WorkerSlot[] = [];
  private concurrency: number;

  constructor(workerPath = defaultWorkerPath(), concurrency = defaultWorkerConcurrency()) {
    this.workerPath = workerPath;
    this.concurrency = concurrency;
  }

  resize(concurrency: number): void {
    this.concurrency = Math.max(1, Math.floor(concurrency));
    while (this.slots.length > this.concurrency) {
      const slot = this.slots.pop();
      if (!slot) {
        break;
      }
      this.rejectSlotTask(slot, new Error("JavaScript diagnostics worker was resized."));
      void slot.worker.terminate();
    }
    this.dispatch();
  }

  async close(): Promise<void> {
    const queued = this.queue.splice(0);
    for (const task of queued) {
      task.reject(new Error("JavaScript diagnostics worker pool is closed."));
    }
    const slots = this.slots.splice(0);
    for (const slot of slots) {
      this.rejectSlotTask(slot, new Error("JavaScript diagnostics worker pool is closed."));
    }
    await Promise.all(slots.map((slot) => slot.worker.terminate()));
  }

  run(
    request: JsDiagnosticsWorkerRequest,
    cancellation: JsDiagnosticsWorkerCancellation,
  ): Promise<JsDiagnosticsWorkerResponse> {
    return new Promise((resolve, reject) => {
      this.queue.push({
        request,
        enqueuedAt: process.hrtime.bigint(),
        payloadBytes: byteLengthOfJson(request),
        isCancellationRequested: () => cancellation.isCancellationRequested(),
        resolve,
        reject,
      });
      this.dispatch();
    });
  }

  private dispatch(): void {
    while (this.queue.length > 0) {
      const slot = this.idleSlot() ?? this.createSlotIfNeeded();
      if (!slot || slot.task) {
        return;
      }
      const task = this.queue.shift();
      if (!task) {
        return;
      }
      if (task.isCancellationRequested?.()) {
        task.resolve({
          id: task.request.id,
          diagnostics: [],
          cancelled: true,
          queueWaitMs: elapsedMs(task.enqueuedAt),
          payloadBytes: task.payloadBytes,
          queueLengthAtDispatch: this.queue.length,
        });
        continue;
      }
      slot.task = task;
      const startedAt = process.hrtime.bigint();
      slot.worker.postMessage(task.request);
      task.resolve = timedWorkerResolve(task, startedAt, this.queue.length, task.resolve);
    }
  }

  private idleSlot(): WorkerSlot | undefined {
    return this.slots.find((slot) => !slot.task);
  }

  private createSlotIfNeeded(): WorkerSlot | undefined {
    if (this.slots.length >= this.concurrency) {
      return undefined;
    }
    const worker = new Worker(this.workerPath);
    worker.unref();
    const slot: WorkerSlot = { worker };
    worker.on("message", (response: JsDiagnosticsWorkerResponse) => {
      const task = slot.task;
      slot.task = undefined;
      if (task) {
        task.resolve(response);
      }
      this.dispatch();
    });
    worker.on("error", (error) => {
      this.replaceFailedSlot(slot, error);
    });
    worker.on("exit", (code) => {
      if (code !== 0 && this.slots.includes(slot)) {
        this.replaceFailedSlot(
          slot,
          new Error(`JavaScript diagnostics worker exited with ${code}.`),
        );
      }
    });
    this.slots.push(slot);
    return slot;
  }

  private replaceFailedSlot(slot: WorkerSlot, error: Error): void {
    this.rejectSlotTask(slot, error);
    const index = this.slots.indexOf(slot);
    if (index !== -1) {
      this.slots.splice(index, 1);
    }
    void slot.worker.terminate();
    this.dispatch();
  }

  private rejectSlotTask(slot: WorkerSlot, error: Error): void {
    const task = slot.task;
    slot.task = undefined;
    task?.reject(error);
  }
}

function timedWorkerResolve(
  task: WorkerTask,
  startedAt: bigint,
  queueLengthAtDispatch: number,
  resolve: WorkerTask["resolve"],
): WorkerTask["resolve"] {
  return (response) => {
    resolve({
      ...response,
      queueWaitMs: elapsedMs(task.enqueuedAt) - elapsedMs(startedAt),
      runMs: elapsedMs(startedAt),
      payloadBytes: task.payloadBytes,
      resultBytes: byteLengthOfJson(response),
      queueLengthAtDispatch,
    });
  };
}

function elapsedMs(startedAt: bigint): number {
  return Number(process.hrtime.bigint() - startedAt) / 1_000_000;
}

function byteLengthOfJson(value: unknown): number {
  try {
    return Buffer.byteLength(JSON.stringify(value), "utf8");
  } catch {
    return 0;
  }
}

function defaultWorkerPath(): string {
  return path.join(__dirname, "js-diagnostics-worker.js");
}

function defaultWorkerConcurrency(): number {
  return Math.max(1, Math.floor(os.availableParallelism() / 2));
}
