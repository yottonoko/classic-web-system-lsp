import { rangeFromOffsets } from "./position";
import { parseVbscriptCst } from "./vbscript-cst";
import type { Range } from "vscode-languageserver-types";
import type {
  AspFlowchartBuildOptions,
  AspFlowchartEdge,
  AspFlowchartInclude,
  AspFlowchartNode,
  AspFlowchartNodeKind,
  AspFlowchartPayload,
  AspFlowchartSection,
  AspParsedDocument,
  AspRegion,
  VbToken,
} from "./types";

interface VbStatement {
  index: number;
  tokens: VbToken[];
  start: number;
  end: number;
}

interface ProcedureBlock {
  declaration: VbStatement;
  startIndex: number;
  endIndex: number;
  label: string;
  kind: "procedure" | "property";
}

interface FlowStatement {
  kind: "statement";
  statement: VbStatement;
  nodeKind: AspFlowchartNodeKind;
  label: string;
  terminates?: boolean;
}

interface FlowIf {
  kind: "if";
  branches: FlowIfBranch[];
}

interface FlowIfBranch {
  kind: "if" | "elseif" | "else";
  statement: VbStatement;
  label: string;
  body: FlowElement[];
}

interface FlowSelect {
  kind: "select";
  statement: VbStatement;
  label: string;
  cases: FlowCase[];
}

interface FlowCase {
  statement: VbStatement;
  label: string;
  body: FlowElement[];
}

interface FlowLoop {
  kind: "loop";
  statement: VbStatement;
  nodeKind: Extract<AspFlowchartNodeKind, "for" | "forEach" | "do" | "while">;
  label: string;
  body: FlowElement[];
}

type FlowElement = FlowStatement | FlowIf | FlowSelect | FlowLoop;

type StopPredicate = (statement: VbStatement) => boolean;

interface ParseResult {
  elements: FlowElement[];
  index: number;
}

interface FlowchartAssembly {
  sections: AspFlowchartSection[];
  nodes: AspFlowchartNode[];
  edges: AspFlowchartEdge[];
  nextNodeIndex: number;
  nextEdgeIndex: number;
}

export function buildAspFlowchart(
  parsed: AspParsedDocument,
  options: AspFlowchartBuildOptions = {},
): AspFlowchartPayload {
  const statements = vbStatements(parsed);
  const procedures = procedureBlocks(statements);
  const procedureStatementIndexes = new Set<number>();
  for (const procedure of procedures) {
    for (let index = procedure.startIndex; index <= procedure.endIndex; index += 1) {
      procedureStatementIndexes.add(index);
    }
  }
  const assembly: FlowchartAssembly = {
    sections: [],
    nodes: [],
    edges: [],
    nextNodeIndex: 0,
    nextEdgeIndex: 0,
  };
  const topLevelStatements = statements.filter(
    (statement) =>
      !procedureStatementIndexes.has(statement.index) &&
      !isProcedureDeclaration(statement) &&
      !isProcedureEnd(statement) &&
      !isClassBoundary(statement),
  );
  addSectionFlow(assembly, parsed, {
    id: "section-top-level",
    label: "Top Level",
    kind: "topLevel",
    statements: topLevelStatements,
  });
  for (const procedure of procedures) {
    addSectionFlow(assembly, parsed, {
      id: `section-${assembly.sections.length}`,
      label: procedure.label,
      kind: procedure.kind,
      range: rangeFromOffsets(parsed.text, procedure.declaration.start, procedure.declaration.end),
      statements: statements.slice(procedure.startIndex + 1, procedure.endIndex),
    });
  }
  const includes = options.includes ?? parsed.includes.map(flowchartIncludeFromParsed);
  const payload = {
    uri: parsed.uri,
    fileName: options.fileName,
    sections: assembly.sections,
    nodes: assembly.nodes,
    edges: assembly.edges,
    includes,
    mermaid: "",
    stats: {
      sections: assembly.sections.length,
      nodes: assembly.nodes.length,
      edges: assembly.edges.length,
      includes: includes.length,
    },
  };
  return {
    ...payload,
    mermaid: mermaidForFlowchart(payload),
  };
}

function flowchartIncludeFromParsed(
  include: AspParsedDocument["includes"][number],
): AspFlowchartInclude {
  return {
    path: include.path,
    mode: include.mode,
    range: include.range,
  };
}

function vbStatements(parsed: AspParsedDocument): VbStatement[] {
  const tokens = vbscriptRegions(parsed)
    .flatMap((region) =>
      parseVbscriptCst(
        parsed.text.slice(region.contentStart, region.contentEnd),
        parsed.text,
        region.contentStart,
      ).tokens.map((token) => ({ token })),
    )
    .sort((left, right) => left.token.start - right.token.start);
  const statements: VbStatement[] = [];
  let current: VbToken[] = [];
  let previousSignificant: VbToken | undefined;
  const flush = (): void => {
    if (current.length === 0) {
      return;
    }
    const first = current[0];
    const last = current.at(-1) ?? first;
    statements.push({
      index: statements.length,
      tokens: current,
      start: first.start,
      end: last.end,
    });
    current = [];
  };
  for (const { token } of tokens) {
    if (token.kind === "whitespace" || token.kind === "comment") {
      continue;
    }
    if (token.kind === "newline") {
      if (previousSignificant?.text !== "_") {
        flush();
      }
      continue;
    }
    if (
      previousSignificant &&
      current.length > 0 &&
      previousSignificant.text !== "_" &&
      statementGapContainsLineBreak(parsed.text, previousSignificant.end, token.start)
    ) {
      flush();
    }
    if (token.text === ":") {
      flush();
      previousSignificant = token;
      continue;
    }
    current.push(token);
    previousSignificant = token;
  }
  flush();
  return statements;
}

function vbscriptRegions(parsed: AspParsedDocument): AspRegion[] {
  return parsed.regions.filter((region) => region.language === "vbscript");
}

function statementGapContainsLineBreak(text: string, start: number, end: number): boolean {
  return text.slice(Math.max(0, start), Math.max(start, end)).includes("\n");
}

function procedureBlocks(statements: VbStatement[]): ProcedureBlock[] {
  const result: ProcedureBlock[] = [];
  const classStack: string[] = [];
  let current:
    | { declaration: VbStatement; label: string; kind: "procedure" | "property" }
    | undefined;
  for (const statement of statements) {
    const declaration = procedureDeclaration(statement, classStack.at(-1));
    if (declaration && !current) {
      current = {
        declaration: statement,
        label: declaration.label,
        kind: declaration.kind,
      };
      continue;
    }
    if (current) {
      if (isProcedureEnd(statement)) {
        result.push({
          declaration: current.declaration,
          startIndex: current.declaration.index,
          endIndex: statement.index,
          label: current.label,
          kind: current.kind,
        });
        current = undefined;
      }
      continue;
    }
    const className = classDeclarationName(statement);
    if (className) {
      classStack.push(className);
    } else if (isEndClass(statement)) {
      classStack.pop();
    }
  }
  if (current) {
    result.push({
      declaration: current.declaration,
      startIndex: current.declaration.index,
      endIndex: statements.length,
      label: current.label,
      kind: current.kind,
    });
  }
  return result;
}

function addSectionFlow(
  assembly: FlowchartAssembly,
  parsed: AspParsedDocument,
  sectionInput: {
    id: string;
    label: string;
    kind: AspFlowchartSection["kind"];
    range?: Range;
    statements: VbStatement[];
  },
): void {
  const section: AspFlowchartSection = {
    id: sectionInput.id,
    label: sectionInput.label,
    kind: sectionInput.kind,
    range: sectionInput.range,
    nodeIds: [],
  };
  assembly.sections.push(section);
  const start = addNode(assembly, section, "start", "Start", sectionInput.range);
  const end = addNode(assembly, section, "end", "End", sectionInput.range);
  const elements = parseElements(sectionInput.statements, 0, () => false).elements;
  const exits = renderElements(assembly, parsed, section, elements, [start.id], end.id);
  for (const exit of exits) {
    addEdge(assembly, section, exit, end.id);
  }
}

function parseElements(
  statements: VbStatement[],
  startIndex: number,
  stop: StopPredicate,
): ParseResult {
  const elements: FlowElement[] = [];
  let index = startIndex;
  while (index < statements.length) {
    const statement = statements[index];
    if (stop(statement)) {
      break;
    }
    if (isMultilineIf(statement)) {
      const parsed = parseMultilineIf(statements, index);
      elements.push(parsed.element);
      index = parsed.index;
      continue;
    }
    if (isSingleLineIf(statement)) {
      elements.push(inlineIfElement(statement));
      index += 1;
      continue;
    }
    if (isSelectCaseStart(statement)) {
      const parsed = parseSelect(statements, index);
      elements.push(parsed.element);
      index = parsed.index;
      continue;
    }
    const loopKind = loopStartKind(statement);
    if (loopKind) {
      const parsed = parseLoop(statements, index, loopKind);
      elements.push(parsed.element);
      index = parsed.index;
      continue;
    }
    if (isBlockTerminator(statement) || isBranchBoundary(statement)) {
      break;
    }
    elements.push(statementElement(statement));
    index += 1;
  }
  return { elements, index };
}

function parseMultilineIf(
  statements: VbStatement[],
  startIndex: number,
): { element: FlowIf; index: number } {
  const branches: FlowIfBranch[] = [];
  let cursor = startIndex;
  let statement = statements[cursor];
  branches.push({
    kind: "if",
    statement,
    label: ifConditionLabel(statement),
    body: [],
  });
  cursor += 1;
  let body = parseElements(statements, cursor, isIfBranchOrEnd);
  branches[0].body = body.elements;
  cursor = body.index;
  while (cursor < statements.length && isElseIf(statements[cursor])) {
    statement = statements[cursor];
    cursor += 1;
    body = parseElements(statements, cursor, isIfBranchOrEnd);
    branches.push({
      kind: "elseif",
      statement,
      label: ifConditionLabel(statement),
      body: body.elements,
    });
    cursor = body.index;
  }
  if (cursor < statements.length && isElse(statements[cursor])) {
    statement = statements[cursor];
    cursor += 1;
    body = parseElements(statements, cursor, isEndIf);
    branches.push({
      kind: "else",
      statement,
      label: "Else",
      body: body.elements,
    });
    cursor = body.index;
  }
  if (cursor < statements.length && isEndIf(statements[cursor])) {
    cursor += 1;
  }
  return { element: { kind: "if", branches }, index: cursor };
}

function inlineIfElement(statement: VbStatement): FlowIf {
  const thenIndex = keywordIndex(statement.tokens, "then");
  const elseIndex = keywordIndex(statement.tokens, "else", thenIndex + 1);
  const trueTokens =
    thenIndex === -1
      ? []
      : statement.tokens.slice(
          thenIndex + 1,
          elseIndex === -1 ? statement.tokens.length : elseIndex,
        );
  const falseTokens = elseIndex === -1 ? [] : statement.tokens.slice(elseIndex + 1);
  const branches: FlowIfBranch[] = [
    {
      kind: "if",
      statement,
      label: ifConditionLabel(statement),
      body: inlineBranchBody(statement, trueTokens),
    },
  ];
  if (falseTokens.length > 0) {
    branches.push({
      kind: "else",
      statement: syntheticStatement(statement, falseTokens),
      label: "Else",
      body: inlineBranchBody(statement, falseTokens),
    });
  }
  return { kind: "if", branches };
}

function inlineBranchBody(parent: VbStatement, tokens: VbToken[]): FlowElement[] {
  const bodyStatement = syntheticStatement(parent, tokens);
  return bodyStatement.tokens.length > 0 ? [statementElement(bodyStatement)] : [];
}

function syntheticStatement(parent: VbStatement, tokens: VbToken[]): VbStatement {
  const significant = tokens.filter(
    (token) => token.kind !== "whitespace" && token.kind !== "newline",
  );
  const first = significant[0] ?? parent.tokens[0];
  const last = significant.at(-1) ?? first;
  return {
    index: parent.index,
    tokens: significant,
    start: first?.start ?? parent.start,
    end: last?.end ?? parent.end,
  };
}

function parseSelect(
  statements: VbStatement[],
  startIndex: number,
): { element: FlowSelect; index: number } {
  const statement = statements[startIndex];
  const cases: FlowCase[] = [];
  let cursor = startIndex + 1;
  while (cursor < statements.length && !isEndSelect(statements[cursor])) {
    if (!isCase(statements[cursor])) {
      cursor += 1;
      continue;
    }
    const caseStatement = statements[cursor];
    cursor += 1;
    const body = parseElements(
      statements,
      cursor,
      (candidate) => isCase(candidate) || isEndSelect(candidate),
    );
    cases.push({
      statement: caseStatement,
      label: caseLabel(caseStatement),
      body: body.elements,
    });
    cursor = body.index;
  }
  if (cursor < statements.length && isEndSelect(statements[cursor])) {
    cursor += 1;
  }
  return {
    element: {
      kind: "select",
      statement,
      label: selectLabel(statement),
      cases,
    },
    index: cursor,
  };
}

function parseLoop(
  statements: VbStatement[],
  startIndex: number,
  nodeKind: Extract<AspFlowchartNodeKind, "for" | "forEach" | "do" | "while">,
): { element: FlowLoop; index: number } {
  const statement = statements[startIndex];
  const stop = loopStopPredicate(nodeKind);
  const body = parseElements(statements, startIndex + 1, stop);
  const index =
    body.index < statements.length && stop(statements[body.index]) ? body.index + 1 : body.index;
  return {
    element: {
      kind: "loop",
      statement,
      nodeKind,
      label: loopLabel(statement),
      body: body.elements,
    },
    index,
  };
}

function statementElement(statement: VbStatement): FlowStatement {
  const nodeKind = statementNodeKind(statement);
  return {
    kind: "statement",
    statement,
    nodeKind,
    label: statementLabel(statement, nodeKind),
    terminates: nodeKind === "exit",
  };
}

function renderElements(
  assembly: FlowchartAssembly,
  parsed: AspParsedDocument,
  section: AspFlowchartSection,
  elements: FlowElement[],
  previousIds: string[],
  sectionEndId: string,
  incomingLabel?: string,
): string[] {
  let exits = previousIds;
  let labelForNext = incomingLabel;
  for (const element of elements) {
    if (element.kind === "statement") {
      const node = addStatementNode(
        assembly,
        parsed,
        section,
        element.statement,
        element.nodeKind,
        element.label,
      );
      connectMany(assembly, section, exits, node.id, labelForNext);
      labelForNext = undefined;
      if (element.terminates) {
        addEdge(assembly, section, node.id, sectionEndId, "Exit");
        exits = [];
      } else {
        exits = [node.id];
      }
    } else if (element.kind === "if") {
      exits = renderIf(assembly, parsed, section, element, exits, sectionEndId, labelForNext);
      labelForNext = undefined;
    } else if (element.kind === "select") {
      exits = renderSelect(assembly, parsed, section, element, exits, sectionEndId, labelForNext);
      labelForNext = undefined;
    } else {
      exits = renderLoop(assembly, parsed, section, element, exits, sectionEndId, labelForNext);
      labelForNext = undefined;
    }
  }
  return exits;
}

function renderIf(
  assembly: FlowchartAssembly,
  parsed: AspParsedDocument,
  section: AspFlowchartSection,
  element: FlowIf,
  previousIds: string[],
  sectionEndId: string,
  incomingLabel?: string,
): string[] {
  const exits: string[] = [];
  let falseSources = previousIds;
  let firstBranch = true;
  for (const branch of element.branches) {
    const nodeKind = branch.kind === "if" ? "if" : branch.kind === "elseif" ? "elseif" : "else";
    const node = addStatementNode(
      assembly,
      parsed,
      section,
      branch.statement,
      nodeKind,
      branch.kind === "else" ? "Else" : `${nodeKind === "if" ? "If" : "ElseIf"} ${branch.label}`,
    );
    connectMany(assembly, section, falseSources, node.id, firstBranch ? incomingLabel : "No");
    firstBranch = false;
    const bodyExits = renderElements(
      assembly,
      parsed,
      section,
      branch.body,
      [node.id],
      sectionEndId,
      branch.kind === "else" ? undefined : "Yes",
    );
    exits.push(...bodyExits);
    falseSources = branch.kind === "else" ? [] : [node.id];
  }
  exits.push(...falseSources);
  return uniqueIds(exits);
}

function renderSelect(
  assembly: FlowchartAssembly,
  parsed: AspParsedDocument,
  section: AspFlowchartSection,
  element: FlowSelect,
  previousIds: string[],
  sectionEndId: string,
  incomingLabel?: string,
): string[] {
  const selectNode = addStatementNode(
    assembly,
    parsed,
    section,
    element.statement,
    "select",
    element.label,
  );
  connectMany(assembly, section, previousIds, selectNode.id, incomingLabel);
  if (element.cases.length === 0) {
    return [selectNode.id];
  }
  const exits: string[] = [];
  for (const branch of element.cases) {
    const caseNode = addStatementNode(
      assembly,
      parsed,
      section,
      branch.statement,
      "case",
      branch.label,
    );
    addEdge(assembly, section, selectNode.id, caseNode.id, branch.label);
    exits.push(
      ...renderElements(assembly, parsed, section, branch.body, [caseNode.id], sectionEndId),
    );
  }
  return uniqueIds(exits);
}

function renderLoop(
  assembly: FlowchartAssembly,
  parsed: AspParsedDocument,
  section: AspFlowchartSection,
  element: FlowLoop,
  previousIds: string[],
  sectionEndId: string,
  incomingLabel?: string,
): string[] {
  const loopNode = addStatementNode(
    assembly,
    parsed,
    section,
    element.statement,
    element.nodeKind,
    element.label,
  );
  connectMany(assembly, section, previousIds, loopNode.id, incomingLabel);
  const bodyExits = renderElements(
    assembly,
    parsed,
    section,
    element.body,
    [loopNode.id],
    sectionEndId,
    "Yes",
  );
  for (const exit of bodyExits) {
    if (exit !== loopNode.id) {
      addEdge(assembly, section, exit, loopNode.id, "Repeat");
    }
  }
  return [loopNode.id];
}

function addStatementNode(
  assembly: FlowchartAssembly,
  parsed: AspParsedDocument,
  section: AspFlowchartSection,
  statement: VbStatement,
  kind: AspFlowchartNodeKind,
  label: string,
): AspFlowchartNode {
  return addNode(
    assembly,
    section,
    kind,
    label,
    rangeFromOffsets(parsed.text, statement.start, statement.end),
  );
}

function addNode(
  assembly: FlowchartAssembly,
  section: AspFlowchartSection,
  kind: AspFlowchartNodeKind,
  label: string,
  range?: Range,
): AspFlowchartNode {
  const node = {
    id: `node-${assembly.nextNodeIndex++}`,
    sectionId: section.id,
    kind,
    label,
    range,
  };
  assembly.nodes.push(node);
  section.nodeIds.push(node.id);
  return node;
}

function connectMany(
  assembly: FlowchartAssembly,
  section: AspFlowchartSection,
  sources: string[],
  target: string,
  label?: string,
): void {
  for (const source of sources) {
    addEdge(assembly, section, source, target, label);
  }
}

function addEdge(
  assembly: FlowchartAssembly,
  section: AspFlowchartSection,
  source: string,
  target: string,
  label?: string,
): void {
  if (source === target) {
    return;
  }
  assembly.edges.push({
    id: `edge-${assembly.nextEdgeIndex++}`,
    sectionId: section.id,
    source,
    target,
    label,
  });
}

function mermaidForFlowchart(payload: Omit<AspFlowchartPayload, "mermaid">): string {
  const lines = ["flowchart TB"];
  const nodesById = new Map(payload.nodes.map((node) => [node.id, node]));
  for (const section of payload.sections) {
    lines.push(`  subgraph ${mermaidId(section.id)}["${escapeMermaidText(section.label)}"]`);
    for (const nodeId of section.nodeIds) {
      const node = nodesById.get(nodeId);
      if (!node) {
        continue;
      }
      lines.push(`    ${mermaidNode(node)}`);
    }
    lines.push("  end");
  }
  for (const edge of payload.edges) {
    lines.push(
      `  ${mermaidId(edge.source)} -->${edge.label ? `|${escapeMermaidEdgeLabel(edge.label)}|` : ""} ${mermaidId(edge.target)}`,
    );
  }
  return lines.join("\n");
}

function mermaidNode(node: AspFlowchartNode): string {
  const id = mermaidId(node.id);
  const label = escapeMermaidText(node.label);
  if (node.kind === "start" || node.kind === "end") {
    return `${id}(["${label}"])`;
  }
  if (
    node.kind === "if" ||
    node.kind === "elseif" ||
    node.kind === "select" ||
    node.kind === "case"
  ) {
    return `${id}{"${label}"}`;
  }
  return `${id}["${label}"]`;
}

function mermaidId(id: string): string {
  return id.replace(/[^A-Za-z0-9_]/g, "_");
}

function escapeMermaidText(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("[", "&#91;")
    .replaceAll("]", "&#93;")
    .replaceAll("{", "&#123;")
    .replaceAll("}", "&#125;")
    .replace(/\s+/g, " ")
    .trim();
}

function escapeMermaidEdgeLabel(value: string): string {
  return escapeMermaidText(value).replaceAll("|", "/");
}

function isProcedureDeclaration(statement: VbStatement): boolean {
  return procedureDeclaration(statement) !== undefined;
}

function procedureDeclaration(
  statement: VbStatement,
  className?: string,
): { label: string; kind: "procedure" | "property" } | undefined {
  const tokens = statement.tokens;
  let index = lower(tokens[0]) === "public" || lower(tokens[0]) === "private" ? 1 : 0;
  const first = lower(tokens[index]);
  if (first === "sub" || first === "function") {
    const name = tokens[index + 1]?.text;
    return name
      ? {
          label: `${titleKeyword(first)} ${className ? `${className}.` : ""}${name}`,
          kind: "procedure",
        }
      : undefined;
  }
  if (first === "property") {
    const accessor = lower(tokens[index + 1]);
    const name = tokens[index + 2]?.text;
    return accessor && name
      ? {
          label: `Property ${titleKeyword(accessor)} ${className ? `${className}.` : ""}${name}`,
          kind: "property",
        }
      : undefined;
  }
  return undefined;
}

function classDeclarationName(statement: VbStatement): string | undefined {
  return lower(statement.tokens[0]) === "class" ? statement.tokens[1]?.text : undefined;
}

function isClassBoundary(statement: VbStatement): boolean {
  return classDeclarationName(statement) !== undefined || isEndClass(statement);
}

function isEndClass(statement: VbStatement): boolean {
  return lower(statement.tokens[0]) === "end" && lower(statement.tokens[1]) === "class";
}

function isProcedureEnd(statement: VbStatement): boolean {
  if (lower(statement.tokens[0]) !== "end") {
    return false;
  }
  return ["sub", "function", "property"].includes(lower(statement.tokens[1]) ?? "");
}

function isMultilineIf(statement: VbStatement): boolean {
  return (
    lower(statement.tokens[0]) === "if" &&
    keywordIndex(statement.tokens, "then") === statement.tokens.length - 1
  );
}

function isSingleLineIf(statement: VbStatement): boolean {
  return (
    lower(statement.tokens[0]) === "if" &&
    keywordIndex(statement.tokens, "then") !== -1 &&
    !isMultilineIf(statement)
  );
}

function isElseIf(statement: VbStatement): boolean {
  return lower(statement.tokens[0]) === "elseif";
}

function isElse(statement: VbStatement): boolean {
  return lower(statement.tokens[0]) === "else";
}

function isEndIf(statement: VbStatement): boolean {
  return lower(statement.tokens[0]) === "end" && lower(statement.tokens[1]) === "if";
}

function isIfBranchOrEnd(statement: VbStatement): boolean {
  return isElseIf(statement) || isElse(statement) || isEndIf(statement);
}

function isSelectCaseStart(statement: VbStatement): boolean {
  return lower(statement.tokens[0]) === "select" && lower(statement.tokens[1]) === "case";
}

function isCase(statement: VbStatement): boolean {
  return lower(statement.tokens[0]) === "case";
}

function isEndSelect(statement: VbStatement): boolean {
  return lower(statement.tokens[0]) === "end" && lower(statement.tokens[1]) === "select";
}

function loopStartKind(
  statement: VbStatement,
): Extract<AspFlowchartNodeKind, "for" | "forEach" | "do" | "while"> | undefined {
  const first = lower(statement.tokens[0]);
  const second = lower(statement.tokens[1]);
  if (first === "for" && second === "each") {
    return "forEach";
  }
  if (first === "for") {
    return "for";
  }
  if (first === "do") {
    return "do";
  }
  if (first === "while") {
    return "while";
  }
  return undefined;
}

function loopStopPredicate(
  kind: Extract<AspFlowchartNodeKind, "for" | "forEach" | "do" | "while">,
): StopPredicate {
  if (kind === "for" || kind === "forEach") {
    return (statement) => lower(statement.tokens[0]) === "next";
  }
  if (kind === "do") {
    return (statement) => lower(statement.tokens[0]) === "loop";
  }
  return (statement) => lower(statement.tokens[0]) === "wend";
}

function isBranchBoundary(statement: VbStatement): boolean {
  return isElseIf(statement) || isElse(statement) || isCase(statement);
}

function isBlockTerminator(statement: VbStatement): boolean {
  const first = lower(statement.tokens[0]);
  return (
    isEndIf(statement) ||
    isEndSelect(statement) ||
    isProcedureEnd(statement) ||
    isEndClass(statement) ||
    first === "next" ||
    first === "loop" ||
    first === "wend"
  );
}

function statementNodeKind(statement: VbStatement): AspFlowchartNodeKind {
  const first = lower(statement.tokens[0]);
  const second = lower(statement.tokens[1]);
  if (first === "call") {
    return "call";
  }
  if (first === "exit") {
    return "exit";
  }
  if (first === "for") {
    return second === "each" ? "forEach" : "for";
  }
  if (first === "do") {
    return "do";
  }
  if (first === "while") {
    return "while";
  }
  return "statement";
}

function statementLabel(statement: VbStatement, kind: AspFlowchartNodeKind): string {
  if (kind === "call") {
    return `Call ${tokensText(statement.tokens.slice(1))}`;
  }
  if (kind === "exit") {
    return tokensText(statement.tokens);
  }
  return tokensText(statement.tokens);
}

function ifConditionLabel(statement: VbStatement): string {
  const thenIndex = keywordIndex(statement.tokens, "then");
  const start = lower(statement.tokens[0]) === "elseif" ? 1 : 1;
  return tokensText(
    statement.tokens.slice(start, thenIndex === -1 ? statement.tokens.length : thenIndex),
  );
}

function selectLabel(statement: VbStatement): string {
  return `Select Case ${tokensText(statement.tokens.slice(2))}`;
}

function caseLabel(statement: VbStatement): string {
  return `Case ${tokensText(statement.tokens.slice(1))}`;
}

function loopLabel(statement: VbStatement): string {
  return tokensText(statement.tokens);
}

function tokensText(tokens: VbToken[]): string {
  return tokens
    .filter((token) => token.kind !== "whitespace" && token.kind !== "newline")
    .map((token) => token.text)
    .join(" ")
    .replace(/\s+([),.])/g, "$1")
    .replace(/([(])\s+/g, "$1")
    .replace(/\s+/g, " ")
    .trim();
}

function keywordIndex(tokens: VbToken[], keyword: string, startIndex = 0): number {
  return tokens.findIndex((token, index) => index >= startIndex && lower(token) === keyword);
}

function lower(token: VbToken | undefined): string | undefined {
  return token?.text.toLowerCase();
}

function titleKeyword(value: string): string {
  return value.charAt(0).toUpperCase() + value.slice(1).toLowerCase();
}

function uniqueIds(ids: string[]): string[] {
  return [...new Set(ids)];
}
