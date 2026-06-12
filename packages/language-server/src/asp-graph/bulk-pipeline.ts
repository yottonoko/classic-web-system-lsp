import type { AspSettings } from "@asp-lsp/core";
import type { BulkWorkerPool } from "./bulk-worker-pool";
import type { SpilledGraphIndexRecord } from "./bulk-protocol";
import {
  buildImplicitGlobalIncludeGraphAsync,
  canonicalizeImplicitGlobalIndexedDocument,
  computeImplicitGlobalCanonicalIds,
  hydrateGraphFileIndex,
  implicitGlobalDocumentMetadataFromIndexed,
  serializableGraphFileIndex,
  type ImplicitGlobalDocumentMetadata,
} from "./implicit-globals";
import { SpillStore, type SpillRecordRef } from "./spill-store";
import type {
  AnalysisCancellation,
  AspGraphDocument,
  AspGraphIndexedDocument,
  GraphFileIndex,
} from "./types";

const graphIndexRecordKind = "graph-index";
const maxBatchFiles = 16;
const maxBatchTextBytes = 8 * 1024 * 1024;

export interface BulkGraphIndexPipelineOptions {
  sources: readonly BulkGraphDocumentSource[];
  settings: AspSettings;
  cancellation: AnalysisCancellation;
  concurrency: number;
  namespace?: string;
  spillDirectory?: string;
  indexDocument(document: AspGraphDocument): Promise<AspGraphIndexedDocument>;
  graphFileKey(fileName: string): string;
  normalizeFileName(fileName: string): string;
  resolveIncludePathDetailsAsync(
    ownerUri: string,
    includePath: string,
    mode: GraphFileIndex["includeRefs"][number]["mode"],
    settings: AspSettings,
  ): Promise<{ fileName: string }>;
  graphFileIndexFingerprint(index: GraphFileIndex["vbSymbolIndex"]): string;
  isCancellationRequested?(): boolean;
  throwIfCancelled?(): void;
  logDebug?(message: string): void;
  workerPool?: Pick<BulkWorkerPool, "writeRecord">;
}

export interface BulkGraphDocumentSource {
  uri: string;
  fileName: string;
  textLength: number;
  load(): Promise<AspGraphDocument>;
}

export interface BulkGraphIndexPipelineResult {
  files: number;
  refs: SpillRecordRef[];
  metadata: ImplicitGlobalDocumentMetadata[];
  canonicalIdById: Map<string, string>;
  bytesWritten: number;
  spillDirectory: string;
  scanCanonicalized(): AsyncIterable<AspGraphIndexedDocument>;
  dispose(): Promise<void>;
}

export async function runSpilledGraphIndexPipeline(
  options: BulkGraphIndexPipelineOptions,
): Promise<BulkGraphIndexPipelineResult> {
  await SpillStore.sweepStaleTemporaryRoots();
  const store = new SpillStore({
    directory: options.spillDirectory,
    namespace: options.namespace,
  });
  const refs: SpillRecordRef[] = [];
  const metadata: ImplicitGlobalDocumentMetadata[] = [];
  let bytesWritten = 0;
  try {
    for (const batch of graphDocumentBatches(options.sources)) {
      checkCancelled(options);
      const results = await mapWithBoundedConcurrency(
        batch,
        options.concurrency,
        async (source) => {
          checkCancelled(options);
          const document = await source.load();
          checkCancelled(options);
          const indexed = await options.indexDocument(document);
          checkCancelled(options);
          const record = spilledGraphIndexRecordFromIndexed(indexed);
          const ref = await writeSpillRecord(options, store, record);
          return {
            ref,
            metadata: implicitGlobalDocumentMetadataFromIndexed(
              indexed,
              options.graphFileKey(indexed.document.fileName),
            ),
          };
        },
      );
      for (const result of results) {
        refs.push(result.ref);
        metadata.push(result.metadata);
        bytesWritten += result.ref.bytes;
      }
    }

    checkCancelled(options);
    const includeGraph = await buildImplicitGlobalIncludeGraphAsync(
      metadata,
      options.settings,
      {
        graphFileKey: options.graphFileKey,
        normalizeFileName: options.normalizeFileName,
        resolveIncludePathDetailsAsync: options.resolveIncludePathDetailsAsync,
      },
      options.cancellation,
    );
    const canonical = computeImplicitGlobalCanonicalIds(
      metadata,
      includeGraph,
      options.cancellation,
    );
    options.logDebug?.(
      `[asp-lsp] asp.graph.bulk.spill.write: files=${refs.length}, bytes=${bytesWritten}`,
    );
    if (canonical.groups > 0) {
      options.logDebug?.(
        `[asp-lsp] asp.graph.implicitGlobals.groups: groups=${canonical.groups}, maxGroupSize=${canonical.maxGroupSize}`,
      );
    }
    return {
      files: refs.length,
      refs,
      metadata,
      canonicalIdById: canonical.canonicalIdById,
      bytesWritten,
      spillDirectory: store.directory,
      scanCanonicalized: () =>
        scanCanonicalizedRecords(store, refs, canonical.canonicalIdById, options),
      dispose: () => store.clear(),
    };
  } catch (error) {
    await store.clear();
    throw error;
  }
}

export function spilledGraphIndexRecordFromIndexed(
  indexed: AspGraphIndexedDocument,
): SpilledGraphIndexRecord {
  return {
    document: indexed.document,
    graphIndex: serializableGraphFileIndex(indexed.graphIndex),
  };
}

export function indexedGraphDocumentFromSpilledRecord(
  record: SpilledGraphIndexRecord,
): AspGraphIndexedDocument {
  return {
    document: record.document,
    graphIndex: hydrateGraphFileIndex(record.graphIndex),
  };
}

async function* scanCanonicalizedRecords(
  store: SpillStore,
  refs: readonly SpillRecordRef[],
  canonicalIdById: Map<string, string>,
  options: Pick<BulkGraphIndexPipelineOptions, "graphFileIndexFingerprint" | "throwIfCancelled">,
): AsyncIterable<AspGraphIndexedDocument> {
  const needsCanonicalization = [...canonicalIdById].some(
    ([id, canonicalId]) => id !== canonicalId,
  );
  for (const ref of refs) {
    options.throwIfCancelled?.();
    const record = await store.readRecord<SpilledGraphIndexRecord>(ref);
    const indexed = indexedGraphDocumentFromSpilledRecord(record);
    yield needsCanonicalization
      ? canonicalizeImplicitGlobalIndexedDocument(
          indexed,
          canonicalIdById,
          options.graphFileIndexFingerprint,
        )
      : indexed;
  }
}

async function writeSpillRecord(
  options: BulkGraphIndexPipelineOptions,
  store: SpillStore,
  record: SpilledGraphIndexRecord,
): Promise<SpillRecordRef> {
  if (options.workerPool) {
    try {
      const response = await options.workerPool.writeRecord(
        store.directory,
        graphIndexRecordKind,
        record,
        {
          isCancellationRequested: options.isCancellationRequested,
        },
      );
      checkCancelled(options);
      if (response.cancelled === true) {
        throw new Error("Bulk spill worker request was cancelled.");
      }
      if (response.error) {
        throw new Error(response.error.message);
      }
      if (response.ref) {
        return response.ref;
      }
      throw new Error("Bulk spill worker returned no record reference.");
    } catch (error) {
      checkCancelled(options);
      options.logDebug?.(`[asp-lsp] asp.graph.bulk.worker.fallback: reason=${errorMessage(error)}`);
    }
  }
  return store.writeRecord(graphIndexRecordKind, record);
}

function graphDocumentBatches(
  sources: readonly BulkGraphDocumentSource[],
): BulkGraphDocumentSource[][] {
  const batches: BulkGraphDocumentSource[][] = [];
  let batch: BulkGraphDocumentSource[] = [];
  let batchBytes = 0;
  for (const source of sources) {
    const textBytes = source.textLength;
    if (
      batch.length > 0 &&
      (batch.length >= maxBatchFiles || batchBytes + textBytes > maxBatchTextBytes)
    ) {
      batches.push(batch);
      batch = [];
      batchBytes = 0;
    }
    batch.push(source);
    batchBytes += textBytes;
  }
  if (batch.length > 0) {
    batches.push(batch);
  }
  return batches;
}

async function mapWithBoundedConcurrency<T, R>(
  items: readonly T[],
  concurrency: number,
  mapper: (item: T) => Promise<R>,
): Promise<R[]> {
  const results = Array.from<R | undefined>({ length: items.length });
  let next = 0;
  const workers = Array.from(
    { length: Math.max(1, Math.min(items.length, concurrency)) },
    async () => {
      for (;;) {
        const index = next;
        next += 1;
        if (index >= items.length) {
          return;
        }
        results[index] = await mapper(items[index]);
      }
    },
  );
  await Promise.all(workers);
  return results as R[];
}

function checkCancelled(options: BulkGraphIndexPipelineOptions): void {
  if (options.throwIfCancelled) {
    options.throwIfCancelled();
    return;
  }
  if (options.cancellation.isCancellationRequested()) {
    throw new Error("Graph generation was cancelled.");
  }
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
