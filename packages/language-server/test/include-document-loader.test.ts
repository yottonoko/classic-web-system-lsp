import { describe, expect, it } from "vitest";
import { IncludeDocumentLoader } from "../src/include-document-loader";

describe("IncludeDocumentLoader", () => {
  it("shares in-flight loads for the same file state", async () => {
    const loader = new IncludeDocumentLoader<string>();
    const fileName = "/site/shared.inc";
    const state = { mtimeMs: 10, size: 42 };
    let loads = 0;
    let finish!: (value: string) => void;
    const pending = new Promise<string>((resolve) => {
      finish = resolve;
    });

    const first = loader.getOrLoad(fileName, state, async () => {
      loads += 1;
      return pending;
    });
    const second = loader.getOrLoad(fileName, state, async () => {
      loads += 1;
      return "duplicate";
    });

    await Promise.resolve();
    expect(loads).toBe(1);
    finish("parsed");
    await expect(Promise.all([first, second])).resolves.toEqual(["parsed", "parsed"]);
    expect(loads).toBe(1);
    await expect(
      loader.getOrLoad(fileName, state, async () => {
        loads += 1;
        return "cached";
      }),
    ).resolves.toBe("parsed");
    expect(loads).toBe(1);
  });

  it("does not let cleared pending loads repopulate the cache", async () => {
    const loader = new IncludeDocumentLoader<string>();
    const fileName = "/site/shared.inc";
    const state = { mtimeMs: 10, size: 42 };
    let loads = 0;
    let finish!: (value: string) => void;
    const pending = new Promise<string>((resolve) => {
      finish = resolve;
    });

    const first = loader.getOrLoad(fileName, state, async () => {
      loads += 1;
      return pending;
    });
    loader.clear();
    finish("stale");
    await expect(first).resolves.toBe("stale");

    await expect(
      loader.getOrLoad(fileName, state, async () => {
        loads += 1;
        return "fresh";
      }),
    ).resolves.toBe("fresh");
    expect(loads).toBe(2);
  });
});
