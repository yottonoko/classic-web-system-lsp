import type { AspGraphDeclarationTypeHint, AspGraphDocument, GraphFileIndex } from "./types";
import type { SpillRecordRef } from "./spill-store";

export type SerializableGraphFileIndex = Omit<GraphFileIndex, "typeHints"> & {
  typeHints: Array<[string, AspGraphDeclarationTypeHint]>;
};

export interface SpilledGraphIndexRecord {
  document: AspGraphDocument;
  graphIndex: SerializableGraphFileIndex;
}

export interface BulkWorkerSpillRecordRequest {
  id: number;
  kind: "spillRecord";
  directory: string;
  recordKind: string;
  record: SpilledGraphIndexRecord;
}

export type BulkWorkerRequest = BulkWorkerSpillRecordRequest;

export interface BulkWorkerResponse {
  id: number;
  ref?: SpillRecordRef;
  runMs?: number;
  cancelled?: boolean;
  error?: {
    name?: string;
    message: string;
    stack?: string;
  };
}
