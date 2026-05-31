import type { AspEmbeddedLanguage, AspSettings } from "@asp-lsp/core";
import type ts from "typescript";

export interface JsDiagnosticsWorkerVirtualDocument {
  uri: string;
  languageId: AspEmbeddedLanguage;
  text: string;
}

export interface JsDiagnosticsWorkerRequest {
  id: number;
  activeVirtual: JsDiagnosticsWorkerVirtualDocument;
  openVirtuals: JsDiagnosticsWorkerVirtualDocument[];
  settings: AspSettings;
  workspaceRoots: string[];
  projectGeneration: number;
  optionOverrides?: Partial<ts.CompilerOptions>;
}

export interface JsDiagnosticsWorkerTiming {
  name: string;
  elapsedMs: number;
}

export interface JsDiagnosticsWorkerTsDiagnostic {
  code: number;
  category: ts.DiagnosticCategory;
  messageText: string;
  start?: number;
  length?: number;
  reportsUnnecessary?: boolean;
}

export interface JsDiagnosticsWorkerResponse {
  id: number;
  diagnostics?: JsDiagnosticsWorkerTsDiagnostic[];
  timings?: JsDiagnosticsWorkerTiming[];
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
