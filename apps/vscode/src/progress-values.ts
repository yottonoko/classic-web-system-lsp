export interface ProgressValue {
  current: number;
  total: number;
}

export function normalizeProgressValue(
  progress: ProgressValue | undefined,
): ProgressValue | undefined {
  if (!progress || !Number.isFinite(progress.current) || !Number.isFinite(progress.total)) {
    return undefined;
  }
  const total = Math.max(0, progress.total);
  const current = Math.max(0, total > 0 ? Math.min(progress.current, total) : progress.current);
  return { current, total };
}

export function progressValueText(progress: ProgressValue | undefined): string {
  const normalized = normalizeProgressValue(progress);
  if (!normalized) {
    return "";
  }
  if (normalized.total <= 0) {
    return normalized.current > 0 ? ` ${normalized.current}/?` : "";
  }
  const percent = Math.round((normalized.current / normalized.total) * 100);
  return ` ${normalized.current}/${normalized.total} (${percent}%)`;
}

export function progressPercentage(progress: ProgressValue | undefined): number {
  const normalized = normalizeProgressValue(progress);
  if (!normalized || normalized.total <= 0) {
    return 0;
  }
  return (normalized.current / normalized.total) * 100;
}

export function aggregateProgressValues(
  values: readonly ProgressValue[],
): ProgressValue | undefined {
  if (values.length === 0) {
    return undefined;
  }
  let current = 0;
  let total = 0;
  let unknownCurrent = 0;
  let hasUnknownTotal = false;
  for (const value of values) {
    const normalized = normalizeProgressValue(value);
    if (!normalized || normalized.total <= 0) {
      hasUnknownTotal = true;
      unknownCurrent += normalized?.current ?? 0;
      continue;
    }
    current += normalized.current;
    total += normalized.total;
  }
  return hasUnknownTotal ? { current: unknownCurrent, total: 0 } : { current, total };
}
