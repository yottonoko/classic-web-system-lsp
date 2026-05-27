import type {
  AspLocale,
  AspParsedDocument,
  AspVbscriptComType,
  AspVbscriptIdentifierCase,
  AspVbscriptIdentifierKind,
  VbExternalRefUsage,
  VbSymbol,
  VbTypeEnvironment,
} from "@asp-lsp/core";
import type { Diagnostic } from "vscode-languageserver-types";

export interface VbDiagnosticsWorkerContext {
  documents?: AspParsedDocument[];
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
  parsed: AspParsedDocument;
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
  error?: {
    name?: string;
    message: string;
    stack?: string;
  };
}
