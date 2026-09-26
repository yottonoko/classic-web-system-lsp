export interface Position {
  line: number;
  character: number;
}

export interface Range {
  start: Position;
  end: Position;
}

export interface TextEdit {
  range: Range;
  newText: string;
}

export interface LineCommentEditsParams {
  textDocument: {
    uri: string;
    version: number;
  };
  selections: Range[];
}

export interface LineCommentEditsResult {
  version: number;
  edits: TextEdit[];
  noOpReason?: "directive-requires-full-document" | "unsafe-structure";
}

export type AspLspServerStatusKind = "idle" | "loading" | "analyzing";

export type AspLspProgressTaskState =
  | "running"
  | "cancelling"
  | "completed"
  | "cancelled"
  | "failed"
  | "stale";

/** A long-running server operation published through `aspLsp/status`. */
export interface AspLspProgressTask {
  id: string;
  kind: Exclude<AspLspServerStatusKind, "idle">;
  label: string;
  detail?: string;
  current?: number;
  total?: number;
  activeItems?: string[];
  cancellable?: boolean;
  state: AspLspProgressTaskState;
  startedAt: number;
  updatedAt: number;
  /** Exact LSP URI of the document whose reference CodeLens is being calculated. */
  documentUri?: string;
  /** Open-document version used to reject stale reference progress after edits. */
  documentVersion?: number;
}

/** Full server progress snapshot published through `aspLsp/status`. */
export interface AspLspStatusNotification {
  status: AspLspServerStatusKind;
  reason?: string;
  progress?: {
    current: number;
    total: number;
  };
  tasks?: AspLspProgressTask[];
}

export type AspFlowchartLabelMode = "normal" | "raw" | "description";

export interface AspFlowchartPayload {
  uri: string;
  fileName?: string;
  labelMode?: AspFlowchartLabelMode;
  sourceText?: string;
  sections: AspFlowchartSection[];
  nodes: AspFlowchartNode[];
  edges: AspFlowchartEdge[];
  includes: AspFlowchartInclude[];
  mermaid: string;
  stats: {
    sections: number;
    nodes: number;
    edges: number;
    includes: number;
  };
}

export interface AspFlowchartIncompletePayload {
  uri: string;
  fileName?: string;
  labelMode?: AspFlowchartLabelMode;
  cancelled?: boolean;
  incomplete: true;
}

export type AspFlowchartResponse = AspFlowchartPayload | AspFlowchartIncompletePayload;

export function isAspFlowchartPayload(value: unknown): value is AspFlowchartPayload {
  if (!value || typeof value !== "object") {
    return false;
  }
  const payload = value as Record<string, unknown>;
  if (payload.incomplete === true || payload.cancelled === true) {
    return false;
  }
  const stats = payload.stats;
  if (!stats || typeof stats !== "object") {
    return false;
  }
  const flowchartStats = stats as Record<string, unknown>;
  return (
    typeof payload.uri === "string" &&
    Array.isArray(payload.sections) &&
    Array.isArray(payload.nodes) &&
    Array.isArray(payload.edges) &&
    Array.isArray(payload.includes) &&
    typeof payload.mermaid === "string" &&
    ["sections", "nodes", "edges", "includes"].every(
      (key) => typeof flowchartStats[key] === "number",
    )
  );
}

export interface AspFlowchartSection {
  id: string;
  label: string;
  kind: "topLevel" | "class" | "procedure" | "property";
  range?: Range;
  nodeIds: string[];
}

export type AspFlowchartNodeKind =
  | "start"
  | "end"
  | "if"
  | "elseif"
  | "else"
  | "select"
  | "case"
  | "for"
  | "forEach"
  | "do"
  | "while"
  | "call"
  | "declaration"
  | "exceptionHandling"
  | "exit"
  | "merge"
  | "output"
  | "statement";

export type AspFlowchartOutputLanguage = "html" | "css" | "javascript" | "text";

export interface AspFlowchartOutputFragment {
  language: AspFlowchartOutputLanguage;
  text: string;
  range?: Range;
}

export interface AspFlowchartNode {
  id: string;
  sectionId: string;
  kind: AspFlowchartNodeKind;
  label: string;
  description?: string;
  links?: AspFlowchartNodeLink[];
  outputFragments?: AspFlowchartOutputFragment[];
  range?: Range;
}

export interface AspFlowchartNodeLink {
  id: string;
  label: string;
  role: "read" | "write" | "call" | "new" | "member" | "definition" | "unknown";
  symbolKind?: string;
  target?: AspFlowchartTarget;
}

export interface AspFlowchartTarget {
  uri: string;
  range?: Range;
  nameRange?: Range;
}

export interface AspFlowchartEdge {
  id: string;
  sectionId: string;
  source: string;
  target: string;
  label?: string;
}

export interface AspFlowchartInclude {
  path: string;
  mode: "file" | "virtual";
  range: Range;
  exists?: boolean;
  resolvedUri?: string;
  actualPath?: string;
  pathCaseMatches?: boolean;
}

export type AspNavigationGraphScope = "document" | "folder" | "workspace";
export type AspNavigationNodeKind = "page" | "fragment" | "external" | "unknown";

export type AspNavigationEdgeKind =
  | "serverRedirect"
  | "htmlAnchor"
  | "htmlFrame"
  | "htmlForm"
  | "metaRefresh"
  | "javascriptLocation"
  | "javascriptHistory"
  | "javascriptFormSubmit";

export type AspNavigationConfidence = "certain" | "probable" | "possible" | "unknown";

export type AspNavigationParameterSource =
  | "queryString"
  | "form"
  | "request"
  | "hiddenInput"
  | "formControl"
  | "literal"
  | "unknown";

export interface AspNavigationGraphPayload {
  scope: AspNavigationGraphScope;
  rootUri?: string;
  correlationId?: string;
  pending?: boolean;
  backgroundTaskId?: string;
  nodes: AspNavigationNode[];
  edges: AspNavigationEdge[];
  stats: {
    documents: number;
    nodes: number;
    edges: number;
    certain: number;
    probable: number;
    possible: number;
    unknown: number;
    external: number;
  };
}

export function isAspNavigationGraphPayload(value: unknown): value is AspNavigationGraphPayload {
  if (!isNavigationGraphRecord(value)) {
    return false;
  }
  const payload = value;
  const stats = payload.stats;
  if (!isNavigationGraphRecord(stats)) {
    return false;
  }
  return (
    isAspNavigationGraphScope(payload.scope) &&
    isOptionalNavigationString(payload, "rootUri") &&
    isOptionalNavigationString(payload, "correlationId") &&
    isOptionalNavigationBoolean(payload, "pending") &&
    isOptionalNavigationString(payload, "backgroundTaskId") &&
    Array.isArray(payload.nodes) &&
    payload.nodes.every(isAspNavigationNode) &&
    Array.isArray(payload.edges) &&
    payload.edges.every(isAspNavigationEdge) &&
    ["documents", "nodes", "edges", "certain", "probable", "possible", "unknown", "external"].every(
      (key) => isNavigationCount(stats[key]),
    )
  );
}

function isNavigationGraphRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isNavigationCount(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0;
}

function isNavigationPosition(value: unknown): value is Position {
  if (!isNavigationGraphRecord(value)) {
    return false;
  }
  return (
    Number.isInteger(value.line) &&
    (value.line as number) >= 0 &&
    Number.isInteger(value.character) &&
    (value.character as number) >= 0
  );
}

function isNavigationRange(value: unknown): value is Range {
  if (!isNavigationGraphRecord(value)) {
    return false;
  }
  const start = value.start;
  const end = value.end;
  if (!isNavigationPosition(start) || !isNavigationPosition(end)) {
    return false;
  }
  return start.line < end.line || (start.line === end.line && start.character <= end.character);
}

function isOptionalNavigationString(record: Record<string, unknown>, key: string): boolean {
  return record[key] === undefined || typeof record[key] === "string";
}

function isOptionalNavigationBoolean(record: Record<string, unknown>, key: string): boolean {
  return record[key] === undefined || typeof record[key] === "boolean";
}

function isOptionalNavigationCount(record: Record<string, unknown>, key: string): boolean {
  return record[key] === undefined || isNavigationCount(record[key]);
}

function isOptionalNavigationRange(record: Record<string, unknown>, key: string): boolean {
  return record[key] === undefined || isNavigationRange(record[key]);
}

function isAspNavigationGraphScope(value: unknown): value is AspNavigationGraphScope {
  return value === "document" || value === "folder" || value === "workspace";
}

function isAspNavigationNodeKind(value: unknown): value is AspNavigationNodeKind {
  return value === "page" || value === "fragment" || value === "external" || value === "unknown";
}

function isAspNavigationEdgeKind(value: unknown): value is AspNavigationEdgeKind {
  return (
    value === "serverRedirect" ||
    value === "htmlAnchor" ||
    value === "htmlFrame" ||
    value === "htmlForm" ||
    value === "metaRefresh" ||
    value === "javascriptLocation" ||
    value === "javascriptHistory" ||
    value === "javascriptFormSubmit"
  );
}

function isAspNavigationConfidence(value: unknown): value is AspNavigationConfidence {
  return value === "certain" || value === "probable" || value === "possible" || value === "unknown";
}

function isAspNavigationParameterSource(value: unknown): value is AspNavigationParameterSource {
  return (
    value === "queryString" ||
    value === "form" ||
    value === "request" ||
    value === "hiddenInput" ||
    value === "formControl" ||
    value === "literal" ||
    value === "unknown"
  );
}

function isAspNavigationNode(value: unknown): value is AspNavigationNode {
  if (!isNavigationGraphRecord(value)) {
    return false;
  }
  return (
    typeof value.id === "string" &&
    isAspNavigationNodeKind(value.kind) &&
    typeof value.label === "string" &&
    isOptionalNavigationString(value, "uri") &&
    isOptionalNavigationString(value, "fileName") &&
    isOptionalNavigationBoolean(value, "exists") &&
    isOptionalNavigationString(value, "externalUrl") &&
    isOptionalNavigationBoolean(value, "isRoot")
  );
}

function isAspNavigationEdge(value: unknown): value is AspNavigationEdge {
  if (!isNavigationGraphRecord(value)) {
    return false;
  }
  return (
    typeof value.id === "string" &&
    typeof value.source === "string" &&
    typeof value.target === "string" &&
    isAspNavigationEdgeKind(value.kind) &&
    isAspNavigationConfidence(value.confidence) &&
    isOptionalNavigationString(value, "label") &&
    isOptionalNavigationString(value, "method") &&
    isOptionalNavigationString(value, "targetFrame") &&
    Array.isArray(value.ranges) &&
    value.ranges.every(isNavigationRange) &&
    (value.parameters === undefined ||
      (Array.isArray(value.parameters) && value.parameters.every(isAspNavigationParameterFlow))) &&
    isOptionalNavigationString(value, "declaredInUri") &&
    Array.isArray(value.evidence) &&
    value.evidence.every(isAspNavigationEvidence) &&
    isOptionalNavigationCount(value, "count")
  );
}

function isAspNavigationEvidence(value: unknown): value is AspNavigationEvidence {
  if (!isNavigationGraphRecord(value)) {
    return false;
  }
  return (
    typeof value.uri === "string" &&
    isNavigationRange(value.range) &&
    isOptionalNavigationRange(value, "valueRange") &&
    typeof value.label === "string" &&
    isOptionalNavigationString(value, "snippet") &&
    (value.extractor === "html" ||
      value.extractor === "vbscript" ||
      value.extractor === "javascript")
  );
}

function isAspNavigationParameterFlow(value: unknown): value is AspNavigationParameterFlow {
  if (!isNavigationGraphRecord(value)) {
    return false;
  }
  return (
    typeof value.name === "string" &&
    isAspNavigationParameterSource(value.source) &&
    isOptionalNavigationString(value, "value") &&
    isOptionalNavigationString(value, "targetUsage") &&
    (value.confidence === undefined || isAspNavigationConfidence(value.confidence)) &&
    isOptionalNavigationRange(value, "range")
  );
}

export interface AspNavigationNode {
  id: string;
  kind: AspNavigationNodeKind;
  label: string;
  uri?: string;
  fileName?: string;
  exists?: boolean;
  externalUrl?: string;
  isRoot?: boolean;
}

export interface AspNavigationEdge {
  id: string;
  source: string;
  target: string;
  kind: AspNavigationEdgeKind;
  label?: string;
  confidence: AspNavigationConfidence;
  method?: string;
  targetFrame?: string;
  ranges: Range[];
  parameters?: AspNavigationParameterFlow[];
  declaredInUri?: string;
  evidence: AspNavigationEvidence[];
  count?: number;
}

export interface AspNavigationEvidence {
  uri: string;
  range: Range;
  valueRange?: Range;
  label: string;
  snippet?: string;
  extractor: "html" | "vbscript" | "javascript";
}

export interface AspNavigationParameterFlow {
  name: string;
  source: AspNavigationParameterSource;
  value?: string;
  targetUsage?: string;
  confidence?: AspNavigationConfidence;
  range?: Range;
}
