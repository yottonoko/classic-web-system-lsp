export interface RestartPolicyOptions {
  baseDelayMs?: number;
  maxDelayMs?: number;
  resetWindowMs?: number;
  now?: () => number;
}

export interface RestartDecision {
  attempt: number;
  delayMs: number;
}

export interface RestartPolicy {
  nextRestart(): RestartDecision;
  reset(): void;
}

const DEFAULT_BASE_DELAY_MS = 500;
const DEFAULT_MAX_DELAY_MS = 10_000;
const DEFAULT_RESET_WINDOW_MS = 60_000;

/**
 * Tracks consecutive server crashes and computes bounded exponential backoff
 * so the language server always recovers instead of permanently stopping.
 */
export function createRestartPolicy(options: RestartPolicyOptions = {}): RestartPolicy {
  const baseDelayMs = options.baseDelayMs ?? DEFAULT_BASE_DELAY_MS;
  const maxDelayMs = options.maxDelayMs ?? DEFAULT_MAX_DELAY_MS;
  const resetWindowMs = options.resetWindowMs ?? DEFAULT_RESET_WINDOW_MS;
  const now = options.now ?? Date.now;

  let consecutiveCrashes = 0;
  let lastCrashAt: number | undefined;

  return {
    nextRestart(): RestartDecision {
      const currentTime = now();
      if (lastCrashAt !== undefined && currentTime - lastCrashAt > resetWindowMs) {
        consecutiveCrashes = 0;
      }
      consecutiveCrashes += 1;
      lastCrashAt = currentTime;

      const delayMs =
        consecutiveCrashes === 1
          ? 0
          : Math.min(maxDelayMs, baseDelayMs * 2 ** (consecutiveCrashes - 2));

      return { attempt: consecutiveCrashes, delayMs };
    },
    reset(): void {
      consecutiveCrashes = 0;
      lastCrashAt = undefined;
    },
  };
}
