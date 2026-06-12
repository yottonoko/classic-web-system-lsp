import path from "node:path";
import { Worker } from "node:worker_threads";
import type {
  BulkWorkerRequest,
  BulkWorkerResponse,
  SpilledGraphIndexRecord,
} from "./bulk-protocol";

interface WorkerSlot {
  worker: Worker;
  task?: WorkerTask;
}

interface WorkerTask {
  request: BulkWorkerRequest;
  isCancellationRequested?: () => boolean;
  resolve(response: BulkWorkerResponse): void;
  reject(error: Error): void;
}

export class BulkWorkerPool {
  private readonly workerPath: string;
  private readonly queue: WorkerTask[] = [];
  private readonly slots: WorkerSlot[] = [];
  private concurrency: number;
  private nextRequestId = 1;

  constructor(workerPath = defaultWorkerPath(), concurrency = 1) {
    this.workerPath = workerPath;
    this.concurrency = Math.max(1, Math.floor(concurrency));
  }

  resize(concurrency: number): void {
    this.concurrency = Math.max(1, Math.floor(concurrency));
    while (this.slots.length > this.concurrency) {
      const slot = this.slots.pop();
      if (!slot) {
        break;
      }
      this.rejectSlotTask(slot, new Error("Bulk worker was resized."));
      void slot.worker.terminate();
    }
    this.dispatch();
  }

  writeRecord(
    directory: string,
    recordKind: string,
    record: SpilledGraphIndexRecord,
    options: { isCancellationRequested?: () => boolean } = {},
  ): Promise<BulkWorkerResponse> {
    return this.run(
      {
        id: this.nextRequestId++,
        kind: "spillRecord",
        directory,
        recordKind,
        record,
      },
      options,
    );
  }

  async close(): Promise<void> {
    const queued = this.queue.splice(0);
    for (const task of queued) {
      task.reject(new Error("Bulk worker pool is closed."));
    }
    const slots = this.slots.splice(0);
    for (const slot of slots) {
      this.rejectSlotTask(slot, new Error("Bulk worker pool is closed."));
    }
    await Promise.all(slots.map((slot) => slot.worker.terminate()));
  }

  private run(
    request: BulkWorkerRequest,
    options: { isCancellationRequested?: () => boolean } = {},
  ): Promise<BulkWorkerResponse> {
    return new Promise((resolve, reject) => {
      this.queue.push({
        request,
        isCancellationRequested: options.isCancellationRequested,
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
        task.resolve({ id: task.request.id, cancelled: true });
        continue;
      }
      slot.task = task;
      slot.worker.postMessage(task.request);
    }
  }

  private idleSlot(): WorkerSlot | undefined {
    return this.slots.find((slot) => !slot.task);
  }

  private createSlotIfNeeded(): WorkerSlot | undefined {
    if (this.slots.length >= this.concurrency) {
      return undefined;
    }
    const worker = new Worker(this.workerPath, {
      resourceLimits: {
        maxOldGenerationSizeMb: 128,
      },
    });
    worker.unref();
    const slot: WorkerSlot = { worker };
    worker.on("message", (response: BulkWorkerResponse) => {
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
        this.replaceFailedSlot(slot, new Error(`Bulk worker exited with ${code}.`));
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

function defaultWorkerPath(): string {
  return path.join(__dirname, "bulk-worker.js");
}
