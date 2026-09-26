import { describe, expect, it } from "vitest";
import {
  aggregateProgressValues,
  normalizeProgressValue,
  progressPercentage,
  progressValueText,
} from "../src/progress-values";

describe("progress values", () => {
  it("clamps an over-complete task before displaying or reporting it", () => {
    expect(normalizeProgressValue({ current: 125, total: 100 })).toEqual({
      current: 100,
      total: 100,
    });
    expect(progressValueText({ current: 125, total: 100 })).toBe(" 100/100 (100%)");
    expect(progressPercentage({ current: 125, total: 100 })).toBe(100);
  });

  it("clamps negative values and rejects non-finite protocol input", () => {
    expect(normalizeProgressValue({ current: -4, total: 10 })).toEqual({
      current: 0,
      total: 10,
    });
    expect(progressValueText({ current: -4, total: 10 })).toBe(" 0/10 (0%)");
    expect(normalizeProgressValue({ current: Number.NaN, total: 10 })).toBeUndefined();
    expect(normalizeProgressValue({ current: 1, total: Number.POSITIVE_INFINITY })).toBeUndefined();
  });

  it("keeps unknown totals indeterminate instead of inventing a percentage", () => {
    expect(normalizeProgressValue({ current: 7, total: 0 })).toEqual({ current: 7, total: 0 });
    expect(progressValueText({ current: 7, total: 0 })).toBe(" 7/?");
    expect(progressPercentage({ current: 7, total: 0 })).toBe(0);
  });

  it("keeps aggregate progress indeterminate while any total is unknown", () => {
    expect(
      aggregateProgressValues([
        { current: 8, total: 10 },
        { current: 3, total: 0 },
      ]),
    ).toEqual({ current: 3, total: 0 });
    expect(progressValueText(aggregateProgressValues([{ current: 3, total: 0 }]))).toBe(" 3/?");
  });

  it("clamps each known task before aggregating", () => {
    expect(
      aggregateProgressValues([
        { current: 12, total: 10 },
        { current: -2, total: 5 },
      ]),
    ).toEqual({ current: 10, total: 15 });
  });
});
