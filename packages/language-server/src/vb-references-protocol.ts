import type { AspSettings, VbReference, VbReferenceOptions, VbSymbolKind } from "@asp-lsp/core";
import type { Range } from "vscode-languageserver-types";

export interface VbReferencesWorkerSource {
  fileName: string;
  mtimeMs: number;
  size: number;
  openVersion?: number;
}

export interface VbReferencesWorkerOpenDocument {
  uri: string;
  fileName: string;
  text: string;
  version: number;
}

export interface VbReferencesWorkerCandidate {
  uri: string;
  fileName: string;
  source: VbReferencesWorkerSource;
}

export interface VbReferencesWorkerTargetSymbol {
  name: string;
  kind: VbSymbolKind;
  sourceUri: string;
  range: Range;
  memberOf?: string;
  scopeName?: string;
  visibility?: "public" | "private";
  procedureKind?: "sub" | "function";
}

export interface VbReferencesWorkerLimits {
  maxDocuments: number;
  maxTextLength: number;
  maxDepth: number;
  includeReadConcurrency: number;
}

export interface VbReferencesWorkerRequest {
  id: number;
  candidate: VbReferencesWorkerCandidate;
  target: VbReferencesWorkerTargetSymbol;
  targets?: VbReferencesWorkerTargetSymbol[];
  settings: AspSettings;
  workspaceRoots: string[];
  openDocuments: VbReferencesWorkerOpenDocument[];
  options: VbReferenceOptions;
  limits: VbReferencesWorkerLimits;
}

export interface VbReferencesWorkerResponse {
  id: number;
  candidate: VbReferencesWorkerCandidate;
  references?: VbReference[];
  referencesByTarget?: Record<string, VbReference[]>;
  fallbackReasons?: string[];
  scannedFiles?: number;
  cacheHits?: number;
  queueWaitMs?: number;
  runMs?: number;
  payloadBytes?: number;
  resultBytes?: number;
  queueLengthAtDispatch?: number;
  cancelled?: boolean;
  error?: {
    name?: string;
    message: string;
    stack?: string;
  };
}
