import type { Range } from "vscode-languageserver/node";
import type {
  AspInclude,
  AspParsedDocument,
  VbGraphExternalSymbol,
  VbSymbolIndex,
} from "@asp-lsp/core";
import type { DiskAnalysisSourceMetadata } from "../disk-analysis-cache";

export type AspGraphScope = "document" | "folder" | "workspace";

export type AspGraphNodeKind =
  | "file"
  | "missingInclude"
  | "vbDeclaration"
  | "vbUnresolved"
  | "vbMemberReference";

export type AspGraphNodeOrigin = "source" | "builtin" | "configured";

export type AspGraphExternalKind = "function" | "constant" | "object" | "member" | "event";

export type AspGraphLinkKind =
  | "include"
  | "declares"
  | "references"
  | "assignments"
  | "calls"
  | "unresolvedReference";

export type AspGraphLinkFilterCategory = AspGraphLinkKind | "member";

export type AspGraphNodeCategory =
  | "root"
  | "file"
  | "missingInclude"
  | "function"
  | "sub"
  | "class"
  | "method"
  | "methodFunction"
  | "methodSub"
  | "property"
  | "member"
  | "globalVariable"
  | "implicitGlobalVariable"
  | "globalConstant"
  | "localVariable"
  | "localConstant"
  | "parameter"
  | "unresolvedFunction"
  | "unresolved";

export interface AspGraphNode {
  id: string;
  kind: AspGraphNodeKind;
  label: string;
  uri?: string;
  fileName?: string;
  range?: Range;
  sourceRange?: Range;
  exists?: boolean;
  declarationKind?: string;
  role?: string;
  receiverName?: string;
  memberName?: string;
  fullPath?: string;
  memberOf?: string;
  bindingScope?: string;
  procedureKind?: string;
  implicit?: boolean;
  implicitGlobal?: boolean;
  implicitGlobalCandidate?: boolean;
  typeName?: string;
  parameters?: AspGraphNodeParameter[];
  arrayKind?: string;
  arrayDimensions?: string[];
  group?: string;
  origin?: AspGraphNodeOrigin;
  externalKind?: AspGraphExternalKind;
  isRoot?: boolean;
}

export interface AspGraphNodeParameter {
  name: string;
  typeName?: string;
  mode?: string;
  optional?: boolean;
}

export interface AspGraphLink {
  id: string;
  source: string;
  target: string;
  kind: AspGraphLinkKind;
  label: string;
  role?: string;
  count: number;
  ranges: Array<{ uri: string; range: Range }>;
  include?: {
    path: string;
    mode: AspInclude["mode"];
    exists: boolean;
    resolvedUri: string;
    actualPath?: string;
    pathCaseMatches?: boolean;
  };
}

export interface AspGraphPayload {
  scope: AspGraphScope;
  rootUri?: string;
  nodes: AspGraphNode[];
  links: AspGraphLink[];
  settings?: {
    initialViewMode: "2d" | "3d";
    hideSingleNodes: boolean;
    hideUnreferencedGlobalSymbols: boolean;
    showOutgoingSelectionLinks: boolean;
    showIncomingDocumentIncludes: boolean;
    showIncomingFolderIncludes: boolean;
    includeRelatedIncludeTreesForUnresolved: boolean;
    hiddenNodeCategories: AspGraphNodeCategory[];
    hiddenLinkCategories: AspGraphLinkFilterCategory[];
  };
  stats: {
    files: number;
    declarations: number;
    references: number;
    assignments: number;
    calls: number;
    unresolvedReferences: number;
    includes: number;
    missingIncludes: number;
    nodes: number;
    links: number;
  };
  truncated?: {
    reason: string;
  };
}

export interface AspGraphDocument {
  uri: string;
  fileName: string;
  text: string;
  source: DiskAnalysisSourceMetadata;
  diskBacked: boolean;
}

export interface AspGraphDocumentCollectionTruncation {
  reason?: string;
}

export interface GraphFileIndex {
  key: string;
  uri: string;
  fileName: string;
  source: DiskAnalysisSourceMetadata;
  includeRefs: AspInclude[];
  vbSymbolIndex: VbSymbolIndex;
  typeHints: Map<string, AspGraphDeclarationTypeHint>;
  fingerprint: string;
  lastUsed: number;
}

export interface AspGraphDeclarationTypeHint {
  typeName?: string;
  parameters?: AspGraphNodeParameter[];
}

export interface AspGraphIndexedDocument {
  document: AspGraphDocument;
  graphIndex: GraphFileIndex;
}

export interface AspGraphBuildState {
  nodes: Map<string, AspGraphNode>;
  links: Map<string, AspGraphLink>;
  declarations: Set<string>;
  sourceDeclarationsByName: Map<string, Array<VbSymbolIndex["declarations"][number]>>;
  sourceDeclarationsById: Map<string, VbSymbolIndex["declarations"][number]>;
  sourceDeclarationFileKeysById: Map<string, string>;
  directIncludesByOwnerKey: Map<string, Array<{ range: Range; targetKey: string }>>;
  parentIncludesByTargetKey: Map<string, Array<{ ownerKey: string; range: Range }>>;
  externalSymbols: AspGraphExternalIndex;
  includeAnalysisTypeDetails: boolean;
  rootUri?: string;
  rootFileKey?: string;
  workspaceRootFileNames: string[];
  stats: AspGraphPayload["stats"];
  truncated?: AspGraphPayload["truncated"];
}

export interface AspGraphExternalIndex {
  byName: Map<string, VbGraphExternalSymbol[]>;
  memberByOwnerAndName: Map<string, VbGraphExternalSymbol>;
}

export interface VbProjectContextLimits {
  maxDocuments: number;
  maxTextLength: number;
}

export type GraphFileIndexOperationCache = Map<string, Promise<GraphFileIndex>>;

export interface WorkspaceVbReferenceExecutionOptions {
  workerMaxDepth?: number;
}

export interface WorkspaceVbReferenceSummaryIncludeGraph {
  directIncludesByOwnerKey: Map<string, Array<{ range: Range; targetKey: string }>>;
  parentIncludesByTargetKey: Map<string, Array<{ ownerKey: string; range: Range }>>;
}

export interface PrecomputedIncludeReachability {
  reachingFileKeysByTarget: Map<string, Set<string>>;
  hasCycle: boolean;
}

export type GraphCancellationToken = { isCancellationRequested?: boolean };

export interface AnalysisCancellation {
  isCancellationRequested(): boolean;
}

export interface FilePublicSignature {
  fingerprint: string;
  defaultLanguage: AspParsedDocument["defaultLanguage"];
  languages: string[];
  exports: unknown[];
  externalRefUsages: unknown[];
  affectsGlobalScope: boolean;
}
