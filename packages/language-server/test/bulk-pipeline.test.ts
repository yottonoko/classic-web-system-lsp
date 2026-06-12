import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, expect, it } from "vitest";
import type { AspInclude, AspSettings, VbSymbolIndex } from "@asp-lsp/core";
import { runSpilledGraphIndexPipeline } from "../src/asp-graph/bulk-pipeline";
import type {
  AnalysisCancellation,
  AspGraphDocument,
  AspGraphIndexedDocument,
  GraphFileIndex,
} from "../src/asp-graph/types";

const settings: AspSettings = { defaultLanguage: "VBScript" };
const neverCancelled: AnalysisCancellation = { isCancellationRequested: () => false };

describe("runSpilledGraphIndexPipeline", () => {
  it("canonicalizes implicit globals while reading indexed documents from spill", async () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-bulk-pipeline-"));
    const common = graphDocument("/site/common.inc", "<% Dim value %>");
    const owner = graphDocument(
      "/site/default.asp",
      '<!--#include file="common.inc"-->\n<% value = 1 %>',
    );
    const commonIndex = graphIndex(common, {
      declarations: [
        declaration("common:value", "value", {
          range: range(0, 7, 12),
          bindingScope: "global",
        }),
      ],
    });
    const ownerIndex = graphIndex(owner, {
      includeRefs: [includeRef("common.inc", range(0, 0, 32))],
      declarations: [
        declaration("owner:value", "value", {
          range: range(1, 3, 8),
          bindingScope: "global",
          implicit: true,
          implicitGlobal: true,
          implicitGlobalCandidate: true,
        }),
      ],
      references: [
        {
          name: "value",
          normalizedName: "value",
          range: range(1, 3, 8),
          role: "write",
          resolvedId: "owner:value",
        },
      ],
    });
    const indexes = new Map([
      [owner.uri, ownerIndex],
      [common.uri, commonIndex],
    ]);
    try {
      const pipeline = await runSpilledGraphIndexPipeline({
        sources: [owner, common].map((document) => ({
          uri: document.uri,
          fileName: document.fileName,
          textLength: document.text.length,
          load: async () => document,
        })),
        settings,
        cancellation: neverCancelled,
        concurrency: 2,
        spillDirectory: directory,
        indexDocument: async (document): Promise<AspGraphIndexedDocument> => ({
          document,
          graphIndex: indexes.get(document.uri) ?? graphIndex(document),
        }),
        graphFileKey: (fileName) => path.normalize(fileName).toLowerCase(),
        normalizeFileName: (fileName) => path.normalize(fileName),
        resolveIncludePathDetailsAsync: async (ownerUri, includePath) => ({
          fileName: path.join(path.dirname(fileNameFromUri(ownerUri)), includePath),
        }),
        graphFileIndexFingerprint: graphFileIndexFingerprint,
      });
      try {
        const firstScan = await collectAsync(pipeline.scanCanonicalized());
        const secondScan = await collectAsync(pipeline.scanCanonicalized());

        expect(firstScan[0].graphIndex.vbSymbolIndex.declarations).toEqual([]);
        expect(firstScan[0].graphIndex.vbSymbolIndex.references[0]?.resolvedId).toBe(
          "common:value",
        );
        expect(firstScan[1].graphIndex.vbSymbolIndex.declarations.map((item) => item.id)).toEqual([
          "common:value",
        ]);
        expect(secondScan.map((item) => item.document.uri)).toEqual(
          firstScan.map((item) => item.document.uri),
        );
      } finally {
        await pipeline.dispose();
      }
      expect(fs.existsSync(directory)).toBe(false);
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });
});

function graphDocument(fileName: string, text: string = ""): AspGraphDocument {
  return {
    uri: `file://${fileName}`,
    fileName,
    text,
    source: { fileName, mtimeMs: 1, size: text.length },
    diskBacked: false,
  };
}

function graphIndex(
  document: AspGraphDocument,
  options: {
    declarations?: VbSymbolIndex["declarations"];
    references?: VbSymbolIndex["references"];
    callSites?: VbSymbolIndex["callSites"];
    deferredExternalRefs?: VbSymbolIndex["deferredExternalRefs"];
    includeRefs?: AspInclude[];
  } = {},
): GraphFileIndex {
  const vbSymbolIndex: VbSymbolIndex = {
    uri: document.uri,
    declarations: options.declarations ?? [],
    references: options.references ?? [],
    callSites: options.callSites ?? [],
    deferredExternalRefs: options.deferredExternalRefs ?? [],
    includeRefs: options.includeRefs ?? [],
    stats: {
      regions: 1,
      tokens: 0,
      declarations: options.declarations?.length ?? 0,
      references: options.references?.length ?? 0,
      callSites: options.callSites?.length ?? 0,
      deferredExternalRefs: options.deferredExternalRefs?.length ?? 0,
    },
  };
  return {
    key: document.fileName,
    uri: document.uri,
    fileName: document.fileName,
    source: document.source,
    includeRefs: options.includeRefs ?? [],
    vbSymbolIndex,
    typeHints: new Map(),
    fingerprint: graphFileIndexFingerprint(vbSymbolIndex),
    lastUsed: 0,
  };
}

function declaration(
  id: string,
  name: string,
  options: Partial<VbSymbolIndex["declarations"][number]>,
): VbSymbolIndex["declarations"][number] {
  const declarationRange = options.range ?? range(0, 0, name.length);
  return {
    id,
    name,
    normalizedName: name.toLowerCase(),
    kind: "variable",
    range: declarationRange,
    nameRange: options.nameRange ?? declarationRange,
    ...options,
  };
}

function includeRef(pathValue: string, includeRange: AspInclude["range"]): AspInclude {
  return {
    path: pathValue,
    mode: "file",
    offset: 0,
    range: includeRange,
    directiveRange: includeRange,
    modeRange: includeRange,
    pathRange: includeRange,
  };
}

function range(line: number, start: number, end: number): AspInclude["range"] {
  return {
    start: { line, character: start },
    end: { line, character: end },
  };
}

function graphFileIndexFingerprint(index: VbSymbolIndex): string {
  return JSON.stringify({
    declarations: index.declarations.map((item) => item.id),
    references: index.references.map((item) => item.resolvedId),
  });
}

function fileNameFromUri(uri: string): string {
  return uri.startsWith("file://") ? uri.slice("file://".length) : uri;
}

async function collectAsync<T>(items: AsyncIterable<T>): Promise<T[]> {
  const collected: T[] = [];
  for await (const item of items) {
    collected.push(item);
  }
  return collected;
}
