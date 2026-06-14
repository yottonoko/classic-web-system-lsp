export interface ClosableWorkerPool {
  close(): Promise<void>;
}
