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

export type VbDiagnosticsWorkerParsedDocument = VbDiagnosticsWorkerDocument &
  Pick<AspParsedDocument, "cst">;

export interface VbDiagnosticsWorkerContext {
  documents?: VbDiagnosticsWorkerDocument[];
  includeSummaryUris?: string[];
  symbols?: VbSymbol[];
  externalRefUsages?: VbExternalRefUsage[];
  typeChecking?: "basic" | "strict";
  ifSyntaxDiagnostics?: NonNullable<NonNullable<AspSettings["vbscript"]>["ifSyntaxDiagnostics"]>;
  identifierCase?: AspVbscriptIdentifierCase;
  identifierCaseByKind?: Partial<Record<AspVbscriptIdentifierKind, AspVbscriptIdentifierCase>>;
  comTypes?: Record<string, AspVbscriptComType>;
  typeEnvironment?: VbTypeEnvironment;
  unusedDiagnostics?: boolean;
  deadCodeDiagnostics?: boolean;
  syntaxSnippets?: boolean;
  syntaxKeywords?: boolean;
  builtinRuntime?: "classicAsp" | "windowsScriptHost";
  locale?: AspLocale;
}

export interface VbDiagnosticsWorkerRequest {
  id: number;
  uri: string;
  text: string;
  settings: AspSettings;
  document?: VbDiagnosticsWorkerParsedDocument;
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
