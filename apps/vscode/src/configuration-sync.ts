export class ConfigurationSyncScheduler {
  private timer: ReturnType<typeof setTimeout> | undefined;
  private pending = false;
  private running = false;
  private disposed = false;

  constructor(
    private readonly delayMs: number,
    private readonly synchronize: () => Promise<void>,
    private readonly handleError: (error: unknown) => void,
  ) {}

  schedule(): void {
    if (this.disposed) {
      return;
    }
    this.pending = true;
    if (this.timer !== undefined) {
      clearTimeout(this.timer);
    }
    this.timer = setTimeout(() => {
      this.timer = undefined;
      void this.flush();
    }, this.delayMs);
  }

  async flush(): Promise<void> {
    if (this.disposed || this.running || !this.pending) {
      return;
    }
    if (this.timer !== undefined) {
      clearTimeout(this.timer);
      this.timer = undefined;
    }
    this.pending = false;
    this.running = true;
    try {
      await this.synchronize();
    } catch (error) {
      this.handleError(error);
    } finally {
      this.running = false;
      if (this.pending) {
        this.schedule();
      }
    }
  }

  cancelPending(): void {
    this.pending = false;
    if (this.timer !== undefined) {
      clearTimeout(this.timer);
      this.timer = undefined;
    }
  }

  dispose(): void {
    this.disposed = true;
    this.cancelPending();
  }
}
