import { afterEach, describe, expect, it, vi } from "vitest";
import { ConfigurationSyncScheduler } from "../src/configuration-sync";

describe("ConfigurationSyncScheduler", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("batches rapid changes into one synchronization", async () => {
    vi.useFakeTimers();
    const synchronize = vi.fn(async () => undefined);
    const scheduler = new ConfigurationSyncScheduler(25, synchronize, () => undefined);

    scheduler.schedule();
    scheduler.schedule();
    scheduler.schedule();
    await vi.advanceTimersByTimeAsync(24);
    expect(synchronize).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    expect(synchronize).toHaveBeenCalledTimes(1);

    scheduler.dispose();
  });

  it("coalesces a change received during synchronization", async () => {
    vi.useFakeTimers();
    let releaseFirst: (() => void) | undefined;
    const synchronize = vi
      .fn<() => Promise<void>>()
      .mockImplementationOnce(
        () =>
          new Promise<void>((resolve) => {
            releaseFirst = resolve;
          }),
      )
      .mockResolvedValue(undefined);
    const scheduler = new ConfigurationSyncScheduler(25, synchronize, () => undefined);

    scheduler.schedule();
    await vi.advanceTimersByTimeAsync(25);
    scheduler.schedule();
    scheduler.schedule();
    releaseFirst?.();
    await Promise.resolve();
    await vi.advanceTimersByTimeAsync(25);
    expect(synchronize).toHaveBeenCalledTimes(2);

    scheduler.dispose();
  });
});
