import { bench, describe } from "vitest";
import { offsetAt, positionAt } from "../src/position";

const fixture = Array.from(
  { length: 4_000 },
  (_, index) => `line ${String(index).padStart(4, "0")} ${"x".repeat(38)}`,
).join("\n");
const offsets = Array.from({ length: 512 }, (_, index) =>
  Math.floor((fixture.length * index) / 512),
);
const positions = offsets.map((offset) => positionAt(fixture, offset));

describe("position helpers", () => {
  bench("positionAt 200KB fixture", () => {
    for (const offset of offsets) {
      positionAt(fixture, offset);
    }
  });

  bench("offsetAt 200KB fixture", () => {
    for (const position of positions) {
      offsetAt(fixture, position);
    }
  });
});
