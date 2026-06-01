import type {
  AspLocale,
  AspParsedDocument,
  AspSettings,
  AspVbscriptComType,
  AspVbscriptIdentifierCase,
  AspVbscriptIdentifierKind,
  VbExternalRefUsage,
  VbSymbol,
  VbTypeEnvironment,
} from "@asp-lsp/core";
import type { Diagnostic } from "vscode-languageserver-types";

export type VbDiagnosticsWorkerDocument = Pick<
  AspParsedDocument,
  | "uri"
  | "text"
  | "regions"
  | "directives"
  | "includes"
  | "serverObjects"
  | "defaultLanguage"
  | "diagnostics"
>;

export interface VbDiagnosticsWorkerContext {
  documents?: VbDiagnosticsWorkerDocument[];
  includeSummaryUris?: string[];
  symbols?: VbSymbol[];
  externalRefUsages?: VbExternalRefUsage[];
  typeChecking?: "basic" | "strict";
  identifierCase?: AspVbscriptIdentifierCase;
  identifierCaseByKind?: Partial<Record<AspVbscriptIdentifierKind, AspVbscriptIdentifierCase>>;
  comTypes?: Record<string, AspVbscriptComType>;
  typeEnvironment?: VbTypeEnvironment;
  unusedDiagnostics?: boolean;
  syntaxSnippets?: boolean;
  locale?: AspLocale;
}

export interface VbDiagnosticsWorkerRequest {
  id: number;
  uri: string;
  text: string;
  settings: AspSettings;
  context: VbDiagnosticsWorkerContext;
}

export interface VbDiagnosticsWorkerTiming {
  name: string;
  elapsedMs: number;
}

export interface VbDiagnosticsWorkerResponse {
  id: number;
  diagnostics?: Diagnostic[];
  timings?: VbDiagnosticsWorkerTiming[];
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
