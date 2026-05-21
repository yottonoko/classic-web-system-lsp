import type { Diagnostic, Position, Range } from "vscode-languageserver-types";

export type AspEmbeddedLanguage = "html" | "css" | "javascript" | "vbscript" | "jscript" | "asp-directive";

export type AspRegionKind =
  | "html"
  | "asp-block"
  | "asp-expression"
  | "asp-directive"
  | "style"
  | "style-attribute"
  | "client-script"
  | "server-script";

export interface AspSettings {
  defaultLanguage?: "VBScript" | "JScript";
  virtualRoot?: string;
  checkJs?: boolean;
  includePaths?: string[];
  legacyEncoding?: string;
}

export interface AspRegion {
  kind: AspRegionKind;
  language: AspEmbeddedLanguage;
  start: number;
  end: number;
  contentStart: number;
  contentEnd: number;
  attributes?: Record<string, string | true>;
}

export interface AspDirective {
  range: Range;
  offset: number;
  name: string;
  attributes: Record<string, string | true>;
}

export interface AspInclude {
  range: Range;
  offset: number;
  path: string;
  mode: "file" | "virtual";
}

export interface AspParsedDocument {
  uri: string;
  text: string;
  regions: AspRegion[];
  directives: AspDirective[];
  includes: AspInclude[];
  defaultLanguage: "VBScript" | "JScript";
  diagnostics: Diagnostic[];
}

export interface SourceMapSegment {
  virtualStart: number;
  virtualEnd: number;
  sourceStart: number;
  sourceEnd: number;
}

export interface SourceMap {
  segments: SourceMapSegment[];
  toSourceOffset(offset: number): number | undefined;
  toVirtualOffset(offset: number): number | undefined;
  toSourcePosition(position: Position): Position | undefined;
  toVirtualPosition(position: Position): Position | undefined;
}

export interface VirtualDocument {
  uri: string;
  languageId: AspEmbeddedLanguage;
  text: string;
  sourceMap: SourceMap;
}
