import { rangeFromOffsets } from "./position";
import { parseVbscriptCst } from "./vbscript-cst";
import type { Range } from "vscode-languageserver-types";
import type {
  AspFlowchartCallSite,
  AspFlowchartBuildOptions,
  AspFlowchartEdge,
  AspFlowchartInclude,
  AspFlowchartNode,
  AspFlowchartNodeKind,
  AspFlowchartNodeLink,
  AspFlowchartPayload,
  AspFlowchartSection,
  AspFlowchartSymbolDeclaration,
  AspFlowchartSymbolDocument,
  AspFlowchartSymbolReference,
  AspLocale,
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

interface ClassBlock {
  declaration: VbStatement;
  startIndex: number;
  endIndex: number;
  label: string;
}

interface FlowchartText {
  topLevel: string;
  start: string;
  end: string;
  else: string;
  yes: string;
  no: string;
  repeat: string;
  exit: string;
  ifCondition(condition: string): string;
  elseifCondition(condition: string): string;
  selectCase(expression: string): string;
  caseBranch(expression: string): string;
  forRange(variable: string, start: string, end: string, step?: string): string;
  forEach(variable: string, collection: string): string;
  loopWhile(condition: string): string;
  loopUntil(condition: string): string;
  repeatLoop(statement: string): string;
  call(name: string, args: string): string;
  assign(target: string, value: string): string;
  declare(symbols: string): string;
  exitStatement(kind: string): string;
  statement(value: string): string;
  symbolRole(role: AspFlowchartNodeLink["role"]): string;
}

interface FlowchartContext {
  locale: AspLocale;
  sourceText: string;
  text: FlowchartText;
  symbols: FlowchartSymbolContext;
}

interface FlowchartSymbolContext {
  currentUri: string;
  documents: AspFlowchartSymbolDocument[];
  declarationsById: Map<string, AspFlowchartResolvedDeclaration>;
  declarationsByName: Map<string, AspFlowchartResolvedDeclaration[]>;
  membersByOwnerAndName: Map<string, AspFlowchartResolvedDeclaration[]>;
}

interface AspFlowchartResolvedDeclaration extends AspFlowchartSymbolDeclaration {
  uri: string;
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

const flowchartLabelLineLength = 28;
const flowchartEdgeLabelLineLength = 22;
const maximumFlowchartLabelCharacters = 180;
const maximumFlowchartEdgeLabelCharacters = 80;

export function buildAspFlowchart(
  parsed: AspParsedDocument,
  options: AspFlowchartBuildOptions = {},
): AspFlowchartPayload {
  const locale = options.locale ?? "en";
  const context = {
    locale,
    sourceText: parsed.text,
    text: flowchartText(locale),
    symbols: createFlowchartSymbolContext(parsed.uri, options.symbols ?? []),
  } satisfies FlowchartContext;
  const statements = vbStatements(parsed);
  const procedures = procedureBlocks(statements);
  const classes = classBlocks(statements);
  const procedureStatementIndexes = new Set<number>();
  for (const procedure of procedures) {
    for (let index = procedure.startIndex; index <= procedure.endIndex; index += 1) {
      procedureStatementIndexes.add(index);
    }
  }
  const classStatementIndexes = new Set<number>();
  for (const classBlock of classes) {
    for (let index = classBlock.startIndex; index <= classBlock.endIndex; index += 1) {
      classStatementIndexes.add(index);
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
      !classStatementIndexes.has(statement.index) &&
      !isProcedureDeclaration(statement) &&
      !isProcedureEnd(statement) &&
      !isClassBoundary(statement),
  );
  addSectionFlow(assembly, parsed, context, {
    id: "section-top-level",
    label: context.text.topLevel,
    kind: "topLevel",
    statements: topLevelStatements,
  });
  for (const classBlock of classes) {
    const statementsInClass = statements
      .slice(classBlock.startIndex + 1, classBlock.endIndex)
      .filter(
        (statement) =>
          !procedureStatementIndexes.has(statement.index) &&
          !isProcedureDeclaration(statement) &&
          !isProcedureEnd(statement) &&
          !isClassBoundary(statement),
      );
    addSectionFlow(assembly, parsed, context, {
      id: `section-${assembly.sections.length}`,
      label: classBlock.label,
      kind: "class",
      range: rangeFromOffsets(
        parsed.text,
        classBlock.declaration.start,
        classBlock.declaration.end,
      ),
      statements: statementsInClass,
    });
  }
  for (const procedure of procedures) {
    addSectionFlow(assembly, parsed, context, {
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

function classBlocks(statements: VbStatement[]): ClassBlock[] {
  const result: ClassBlock[] = [];
  let current: { declaration: VbStatement; label: string } | undefined;
  for (const statement of statements) {
    const className = classDeclarationName(statement);
    if (className && !current) {
      current = {
        declaration: statement,
        label: `Class ${className}`,
      };
      continue;
    }
    if (current && isEndClass(statement)) {
      result.push({
        declaration: current.declaration,
        startIndex: current.declaration.index,
        endIndex: statement.index,
        label: current.label,
      });
      current = undefined;
    }
  }
  if (current) {
    result.push({
      declaration: current.declaration,
      startIndex: current.declaration.index,
      endIndex: statements.length,
      label: current.label,
    });
  }
  return result;
}

function addSectionFlow(
  assembly: FlowchartAssembly,
  parsed: AspParsedDocument,
  context: FlowchartContext,
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
  const start = addNode(assembly, section, "start", context.text.start, sectionInput.range);
  const end = addNode(assembly, section, "end", context.text.end, sectionInput.range);
  const elements = parseElements(sectionInput.statements, 0, () => false, context).elements;
  const exits = renderElements(assembly, parsed, context, section, elements, [start.id], end.id);
  for (const exit of exits) {
    addEdge(assembly, section, exit, end.id);
  }
}

function parseElements(
  statements: VbStatement[],
  startIndex: number,
  stop: StopPredicate,
  context: FlowchartContext,
): ParseResult {
  const elements: FlowElement[] = [];
  let index = startIndex;
  while (index < statements.length) {
    const statement = statements[index];
    if (stop(statement)) {
      break;
    }
    if (isMultilineIf(statement)) {
      const parsed = parseMultilineIf(statements, index, context);
      elements.push(parsed.element);
      index = parsed.index;
      continue;
    }
    if (isSingleLineIf(statement)) {
      elements.push(inlineIfElement(statement, context));
      index += 1;
      continue;
    }
    if (isSelectCaseStart(statement)) {
      const parsed = parseSelect(statements, index, context);
      elements.push(parsed.element);
      index = parsed.index;
      continue;
    }
    const loopKind = loopStartKind(statement);
    if (loopKind) {
      const parsed = parseLoop(statements, index, loopKind, context);
      elements.push(parsed.element);
      index = parsed.index;
      continue;
    }
    if (isBlockTerminator(statement) || isBranchBoundary(statement)) {
      break;
    }
    elements.push(statementElement(statement, context));
    index += 1;
  }
  return { elements, index };
}

function parseMultilineIf(
  statements: VbStatement[],
  startIndex: number,
  context: FlowchartContext,
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
  let body = parseElements(statements, cursor, isIfBranchOrEnd, context);
  branches[0].body = body.elements;
  cursor = body.index;
  while (cursor < statements.length && isElseIf(statements[cursor])) {
    statement = statements[cursor];
    cursor += 1;
    body = parseElements(statements, cursor, isIfBranchOrEnd, context);
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
    body = parseElements(statements, cursor, isEndIf, context);
    branches.push({
      kind: "else",
      statement,
      label: context.text.else,
      body: body.elements,
    });
    cursor = body.index;
  }
  if (cursor < statements.length && isEndIf(statements[cursor])) {
    cursor += 1;
  }
  return { element: { kind: "if", branches }, index: cursor };
}

function inlineIfElement(statement: VbStatement, context: FlowchartContext): FlowIf {
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
      body: inlineBranchBody(statement, trueTokens, context),
    },
  ];
  if (falseTokens.length > 0) {
    branches.push({
      kind: "else",
      statement: syntheticStatement(statement, falseTokens),
      label: context.text.else,
      body: inlineBranchBody(statement, falseTokens, context),
    });
  }
  return { kind: "if", branches };
}

function inlineBranchBody(
  parent: VbStatement,
  tokens: VbToken[],
  context: FlowchartContext,
): FlowElement[] {
  const bodyStatement = syntheticStatement(parent, tokens);
  return bodyStatement.tokens.length > 0 ? [statementElement(bodyStatement, context)] : [];
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
  context: FlowchartContext,
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
      context,
    );
    cases.push({
      statement: caseStatement,
      label: caseLabel(caseStatement, context),
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
      label: selectLabel(statement, context),
      cases,
    },
    index: cursor,
  };
}

function parseLoop(
  statements: VbStatement[],
  startIndex: number,
  nodeKind: Extract<AspFlowchartNodeKind, "for" | "forEach" | "do" | "while">,
  context: FlowchartContext,
): { element: FlowLoop; index: number } {
  const statement = statements[startIndex];
  const stop = loopStopPredicate(nodeKind);
  const body = parseElements(statements, startIndex + 1, stop, context);
  const index =
    body.index < statements.length && stop(statements[body.index]) ? body.index + 1 : body.index;
  return {
    element: {
      kind: "loop",
      statement,
      nodeKind,
      label: loopLabel(statement, context),
      body: body.elements,
    },
    index,
  };
}

function statementElement(statement: VbStatement, context: FlowchartContext): FlowStatement {
  const nodeKind = statementNodeKind(statement);
  const range = statementRange(statement, context);
  return {
    kind: "statement",
    statement,
    nodeKind,
    label: statementLabel(statement, nodeKind, context, range),
    terminates: nodeKind === "exit",
  };
}

function renderElements(
  assembly: FlowchartAssembly,
  parsed: AspParsedDocument,
  context: FlowchartContext,
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
        context,
        section,
        element.statement,
        element.nodeKind,
        element.label,
      );
      connectMany(assembly, section, exits, node.id, labelForNext);
      labelForNext = undefined;
      if (element.terminates) {
        addEdge(assembly, section, node.id, sectionEndId, context.text.exit);
        exits = [];
      } else {
        exits = [node.id];
      }
    } else if (element.kind === "if") {
      exits = renderIf(
        assembly,
        parsed,
        context,
        section,
        element,
        exits,
        sectionEndId,
        labelForNext,
      );
      labelForNext = undefined;
    } else if (element.kind === "select") {
      exits = renderSelect(
        assembly,
        parsed,
        context,
        section,
        element,
        exits,
        sectionEndId,
        labelForNext,
      );
      labelForNext = undefined;
    } else {
      exits = renderLoop(
        assembly,
        parsed,
        context,
        section,
        element,
        exits,
        sectionEndId,
        labelForNext,
      );
      labelForNext = undefined;
    }
  }
  return exits;
}

function renderIf(
  assembly: FlowchartAssembly,
  parsed: AspParsedDocument,
  context: FlowchartContext,
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
      context,
      section,
      branch.statement,
      nodeKind,
      branch.kind === "else"
        ? context.text.else
        : nodeKind === "if"
          ? context.text.ifCondition(branch.label)
          : context.text.elseifCondition(branch.label),
    );
    connectMany(
      assembly,
      section,
      falseSources,
      node.id,
      firstBranch ? incomingLabel : context.text.no,
    );
    firstBranch = false;
    const bodyExits = renderElements(
      assembly,
      parsed,
      context,
      section,
      branch.body,
      [node.id],
      sectionEndId,
      branch.kind === "else" ? undefined : context.text.yes,
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
  context: FlowchartContext,
  section: AspFlowchartSection,
  element: FlowSelect,
  previousIds: string[],
  sectionEndId: string,
  incomingLabel?: string,
): string[] {
  const selectNode = addStatementNode(
    assembly,
    parsed,
    context,
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
      context,
      section,
      branch.statement,
      "case",
      branch.label,
    );
    addEdge(assembly, section, selectNode.id, caseNode.id, branch.label);
    exits.push(
      ...renderElements(
        assembly,
        parsed,
        context,
        section,
        branch.body,
        [caseNode.id],
        sectionEndId,
      ),
    );
  }
  return uniqueIds(exits);
}

function renderLoop(
  assembly: FlowchartAssembly,
  parsed: AspParsedDocument,
  context: FlowchartContext,
  section: AspFlowchartSection,
  element: FlowLoop,
  previousIds: string[],
  sectionEndId: string,
  incomingLabel?: string,
): string[] {
  const loopNode = addStatementNode(
    assembly,
    parsed,
    context,
    section,
    element.statement,
    element.nodeKind,
    element.label,
  );
  connectMany(assembly, section, previousIds, loopNode.id, incomingLabel);
  const bodyExits = renderElements(
    assembly,
    parsed,
    context,
    section,
    element.body,
    [loopNode.id],
    sectionEndId,
    context.text.yes,
  );
  for (const exit of bodyExits) {
    if (exit !== loopNode.id) {
      addEdge(assembly, section, exit, loopNode.id, context.text.repeat);
    }
  }
  return [loopNode.id];
}

function addStatementNode(
  assembly: FlowchartAssembly,
  parsed: AspParsedDocument,
  context: FlowchartContext,
  section: AspFlowchartSection,
  statement: VbStatement,
  kind: AspFlowchartNodeKind,
  label: string,
): AspFlowchartNode {
  const range = rangeFromOffsets(parsed.text, statement.start, statement.end);
  return addNode(assembly, section, kind, label, range, {
    description: statementDescription(statement, kind, context, range),
    links: statementLinks(context, range),
  });
}

function addNode(
  assembly: FlowchartAssembly,
  section: AspFlowchartSection,
  kind: AspFlowchartNodeKind,
  label: string,
  range?: Range,
  options: Pick<AspFlowchartNode, "description" | "links"> = {},
): AspFlowchartNode {
  const node = {
    id: `node-${assembly.nextNodeIndex++}`,
    sectionId: section.id,
    kind,
    label,
    range,
    ...options,
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
    lines.push(`  subgraph ${mermaidId(section.id)}["${mermaidLabel(section.label)}"]`);
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
  const label = mermaidLabel(node.label);
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
    .trim();
}

function escapeMermaidEdgeLabel(value: string): string {
  return mermaidLabel(value, {
    lineLength: flowchartEdgeLabelLineLength,
    maximumCharacters: maximumFlowchartEdgeLabelCharacters,
  }).replaceAll("|", "/");
}

function mermaidLabel(
  value: string,
  options: { lineLength?: number; maximumCharacters?: number } = {},
): string {
  const normalized = value.replace(/\s+/g, " ").trim();
  const clipped = clipFlowchartLabel(
    normalized,
    options.maximumCharacters ?? maximumFlowchartLabelCharacters,
  );
  const lines = wrapFlowchartLabel(clipped, options.lineLength ?? flowchartLabelLineLength);
  return (lines.length > 0 ? lines : [""]).map(escapeMermaidText).join("<br/>");
}

function clipFlowchartLabel(value: string, maximumCharacters: number): string {
  const characters = Array.from(value);
  if (characters.length <= maximumCharacters) {
    return value;
  }
  return `${characters.slice(0, Math.max(0, maximumCharacters - 3)).join("")}...`;
}

function wrapFlowchartLabel(value: string, lineLength: number): string[] {
  const lines: string[] = [];
  let current = "";
  for (const word of value.split(" ")) {
    if (!word) {
      continue;
    }
    for (const part of flowchartWordParts(word, lineLength)) {
      if (!current) {
        current = part;
        continue;
      }
      const next = `${current} ${part}`;
      if (Array.from(next).length <= lineLength) {
        current = next;
      } else {
        lines.push(current);
        current = part;
      }
    }
  }
  if (current) {
    lines.push(current);
  }
  return lines;
}

function flowchartWordParts(value: string, lineLength: number): string[] {
  const characters = Array.from(value);
  if (characters.length <= lineLength) {
    return [value];
  }
  const parts: string[] = [];
  for (let index = 0; index < characters.length; index += lineLength) {
    parts.push(characters.slice(index, index + lineLength).join(""));
  }
  return parts;
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
  if (first === "exit") {
    return "exit";
  }
  if (isDeclarationStatement(statement)) {
    return "declaration";
  }
  if (first === "call" || callParts(statement)) {
    return "call";
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

function statementLabel(
  statement: VbStatement,
  kind: AspFlowchartNodeKind,
  context: FlowchartContext,
  range: Range = statementRange(statement, context),
): string {
  if (kind === "declaration") {
    const declarations = statementDeclarations(context, range);
    if (declarations.length > 0) {
      return context.text.declare(flowchartDeclarationListLabel(declarations, context));
    }
    const declaration = declarationParts(statement);
    return context.text.declare(`${declaration.kind} ${declaration.names}`);
  }
  const assignment = assignmentParts(statement);
  if (assignment) {
    const declaration = statementReferenceDeclaration(context, range, assignment.target, "write");
    return context.text.assign(
      flowchartSymbolLabel(assignment.target, declaration, context),
      flowchartExpressionLabel(
        context,
        range,
        assignment.value,
        new Set(["read", "new", "member"]),
      ),
    );
  }
  const call = callParts(statement);
  if (kind === "call" && call) {
    const declaration = statementCallDeclaration(context, range, call.name);
    return context.text.call(
      flowchartSymbolLabel(call.name, declaration, context),
      flowchartExpressionLabel(context, range, call.args, new Set(["read", "new", "member"])),
    );
  }
  if (kind === "exit") {
    return context.text.exitStatement(tokensText(statement.tokens.slice(1)));
  }
  return context.text.statement(tokensText(statement.tokens));
}

function statementDescription(
  statement: VbStatement,
  kind: AspFlowchartNodeKind,
  context: FlowchartContext,
  range: Range = statementRange(statement, context),
): string {
  return statementLabel(statement, kind, context, range);
}

function ifConditionLabel(statement: VbStatement): string {
  const thenIndex = keywordIndex(statement.tokens, "then");
  const start = lower(statement.tokens[0]) === "elseif" ? 1 : 1;
  return tokensText(
    statement.tokens.slice(start, thenIndex === -1 ? statement.tokens.length : thenIndex),
  );
}

function selectLabel(statement: VbStatement, context: FlowchartContext): string {
  return context.text.selectCase(tokensText(statement.tokens.slice(2)));
}

function caseLabel(statement: VbStatement, context: FlowchartContext): string {
  return context.text.caseBranch(tokensText(statement.tokens.slice(1)));
}

function loopLabel(statement: VbStatement, context: FlowchartContext): string {
  const first = lower(statement.tokens[0]);
  const second = lower(statement.tokens[1]);
  if (first === "for" && second === "each") {
    const inIndex = keywordIndex(statement.tokens, "in", 2);
    if (inIndex !== -1) {
      return context.text.forEach(
        tokensText(statement.tokens.slice(2, inIndex)),
        tokensText(statement.tokens.slice(inIndex + 1)),
      );
    }
  }
  if (first === "for") {
    const equalsIndex = tokenTextIndex(statement.tokens, "=", 1);
    const toIndex = keywordIndex(statement.tokens, "to", equalsIndex + 1);
    const stepIndex = keywordIndex(statement.tokens, "step", toIndex + 1);
    if (equalsIndex !== -1 && toIndex !== -1) {
      return context.text.forRange(
        tokensText(statement.tokens.slice(1, equalsIndex)),
        tokensText(statement.tokens.slice(equalsIndex + 1, toIndex)),
        tokensText(statement.tokens.slice(toIndex + 1, stepIndex === -1 ? undefined : stepIndex)),
        stepIndex === -1 ? undefined : tokensText(statement.tokens.slice(stepIndex + 1)),
      );
    }
  }
  if (first === "do" && (second === "while" || second === "until")) {
    const condition = tokensText(statement.tokens.slice(2));
    return second === "while"
      ? context.text.loopWhile(condition)
      : context.text.loopUntil(condition);
  }
  if (first === "while") {
    return context.text.loopWhile(tokensText(statement.tokens.slice(1)));
  }
  return context.text.repeatLoop(tokensText(statement.tokens));
}

function assignmentParts(statement: VbStatement): { target: string; value: string } | undefined {
  const first = lower(statement.tokens[0]);
  const startIndex = first === "set" || first === "let" ? 1 : 0;
  const equalsIndex = tokenTextIndex(statement.tokens, "=", startIndex);
  if (equalsIndex <= startIndex || equalsIndex === statement.tokens.length - 1) {
    return undefined;
  }
  if (["if", "elseif", "for", "select", "case", "do", "while"].includes(first ?? "")) {
    return undefined;
  }
  const target = tokensText(statement.tokens.slice(startIndex, equalsIndex));
  const value = tokensText(statement.tokens.slice(equalsIndex + 1));
  return target && value ? { target, value } : undefined;
}

function declarationParts(statement: VbStatement): { kind: string; names: string } {
  const tokens = statement.tokens;
  let index =
    lower(tokens[0]) === "public" || lower(tokens[0]) === "private" || lower(tokens[0]) === "dim"
      ? lower(tokens[0]) === "dim"
        ? 0
        : 1
      : 0;
  const kind = lower(tokens[index]) ?? "";
  if (kind === "public" || kind === "private") {
    index += 1;
  }
  const nameStart =
    lower(tokens[index]) === "redim" && lower(tokens[index + 1]) === "preserve"
      ? index + 2
      : index + 1;
  return {
    kind: titleKeyword(kind || "variable"),
    names: tokensText(tokens.slice(nameStart)) || tokensText(tokens),
  };
}

function isDeclarationStatement(statement: VbStatement): boolean {
  const first = lower(statement.tokens[0]);
  const second = lower(statement.tokens[1]);
  return (
    first === "dim" ||
    first === "redim" ||
    first === "const" ||
    ((first === "public" || first === "private") &&
      (second === "dim" || second === "const" || second === "redim"))
  );
}

function callParts(statement: VbStatement): { name: string; args: string } | undefined {
  const first = lower(statement.tokens[0]);
  const startIndex = first === "call" ? 1 : 0;
  if (assignmentParts(statement) || isDeclarationStatement(statement)) {
    return undefined;
  }
  const nameEnd = callNameEndIndex(statement.tokens, startIndex);
  if (nameEnd <= startIndex) {
    return undefined;
  }
  const name = tokensText(statement.tokens.slice(startIndex, nameEnd));
  const args = callArgumentsText(statement.tokens.slice(nameEnd));
  if (
    !name ||
    (!args && first !== "call" && !looksLikeArgumentlessCall(statement.tokens, nameEnd))
  ) {
    return undefined;
  }
  return { name, args };
}

function callNameEndIndex(tokens: VbToken[], startIndex: number): number {
  if (tokens[startIndex]?.kind !== "identifier") {
    return startIndex;
  }
  let index = startIndex + 1;
  while (tokens[index]?.text === "." && tokens[index + 1]?.kind === "identifier") {
    index += 2;
  }
  return index;
}

function callArgumentsText(tokens: VbToken[]): string {
  const trimmed = trimTokens(tokens);
  if (trimmed[0]?.text === "(" && trimmed.at(-1)?.text === ")") {
    return tokensText(trimmed.slice(1, -1));
  }
  return tokensText(trimmed);
}

function looksLikeArgumentlessCall(tokens: VbToken[], nameEnd: number): boolean {
  return tokens[nameEnd]?.text === "(" && tokens[nameEnd + 1]?.text === ")";
}

function trimTokens(tokens: VbToken[]): VbToken[] {
  let start = 0;
  let end = tokens.length;
  while (start < end && (tokens[start].kind === "whitespace" || tokens[start].kind === "newline")) {
    start += 1;
  }
  while (
    end > start &&
    (tokens[end - 1].kind === "whitespace" || tokens[end - 1].kind === "newline")
  ) {
    end -= 1;
  }
  return tokens.slice(start, end);
}

function statementLinks(context: FlowchartContext, statementRange: Range): AspFlowchartNodeLink[] {
  const current = context.symbols.documents.find(
    (document) => document.uri === context.symbols.currentUri,
  );
  if (!current) {
    return [];
  }
  const links: AspFlowchartNodeLink[] = [];
  for (const reference of current.references ?? []) {
    if (!rangeContainsRange(statementRange, reference.range)) {
      continue;
    }
    const declaration = resolveFlowchartReference(context.symbols, reference);
    if (declaration) {
      links.push(flowchartLink(context, reference.name, reference.role, declaration));
    }
  }
  for (const callSite of current.callSites ?? []) {
    if (!rangeContainsRange(statementRange, callSite.range)) {
      continue;
    }
    const declaration = resolveFlowchartCallSite(context.symbols, callSite);
    if (declaration) {
      links.push(flowchartLink(context, flowchartCallSiteLabel(callSite), "call", declaration));
    }
  }
  return dedupeFlowchartLinks(links);
}

function statementRange(statement: VbStatement, context: FlowchartContext): Range {
  return rangeFromOffsets(context.sourceText, statement.start, statement.end);
}

function statementDeclarations(
  context: FlowchartContext,
  statementRange: Range,
): AspFlowchartResolvedDeclaration[] {
  const current = currentFlowchartSymbolDocument(context);
  if (!current) {
    return [];
  }
  return current.declarations
    .filter((declaration) => rangeContainsRange(statementRange, declaration.nameRange))
    .map((declaration) => context.symbols.declarationsById.get(declaration.id))
    .filter((declaration): declaration is AspFlowchartResolvedDeclaration => Boolean(declaration));
}

function statementReferenceDeclaration(
  context: FlowchartContext,
  statementRange: Range,
  name: string,
  role?: AspFlowchartSymbolReference["role"],
): AspFlowchartResolvedDeclaration | undefined {
  const current = currentFlowchartSymbolDocument(context);
  if (!current) {
    return undefined;
  }
  const normalizedName = normalizeFlowchartName(name);
  return (current.references ?? [])
    .filter((reference) => rangeContainsRange(statementRange, reference.range))
    .map((reference) => ({
      reference,
      declaration: resolveFlowchartReference(context.symbols, reference),
    }))
    .find(
      ({ reference, declaration }) =>
        declaration &&
        (!role || reference.role === role) &&
        (normalizeFlowchartName(reference.name) === normalizedName ||
          normalizeFlowchartName(reference.memberName ?? "") === normalizedName ||
          normalizedName.endsWith(`.${normalizeFlowchartName(reference.name)}`) ||
          normalizedName.endsWith(`.${normalizeFlowchartName(reference.memberName ?? "")}`)),
    )?.declaration;
}

function statementCallDeclaration(
  context: FlowchartContext,
  statementRange: Range,
  name: string,
): AspFlowchartResolvedDeclaration | undefined {
  const current = currentFlowchartSymbolDocument(context);
  if (!current) {
    return undefined;
  }
  const normalizedName = normalizeFlowchartName(name);
  return (current.callSites ?? [])
    .filter((callSite) => rangeContainsRange(statementRange, callSite.range))
    .map((callSite) => ({
      callSite,
      declaration: resolveFlowchartCallSite(context.symbols, callSite),
    }))
    .find(
      ({ callSite, declaration }) =>
        declaration &&
        (normalizeFlowchartName(callSite.name) === normalizedName ||
          normalizeFlowchartName(callSite.memberName ?? "") === normalizedName ||
          normalizedName.endsWith(`.${normalizeFlowchartName(callSite.name)}`) ||
          normalizedName.endsWith(`.${normalizeFlowchartName(callSite.memberName ?? "")}`)),
    )?.declaration;
}

function flowchartExpressionLabel(
  context: FlowchartContext,
  statementRange: Range,
  expression: string,
  roles: Set<AspFlowchartSymbolReference["role"]>,
): string {
  if (!expression) {
    return expression;
  }
  const current = currentFlowchartSymbolDocument(context);
  if (!current) {
    return expression;
  }
  const seen = new Set<string>();
  const replacements = (current.references ?? [])
    .filter(
      (reference) =>
        roles.has(reference.role) && rangeContainsRange(statementRange, reference.range),
    )
    .map((reference) => ({
      source: reference.memberName ?? reference.name,
      target: flowchartSymbolLabel(
        reference.memberName ?? reference.name,
        resolveFlowchartReference(context.symbols, reference),
        context,
      ),
    }))
    .filter((replacement) => {
      if (!replacement.source || replacement.source === replacement.target) {
        return false;
      }
      const key = normalizeFlowchartName(replacement.source);
      if (seen.has(key)) {
        return false;
      }
      seen.add(key);
      return true;
    })
    .sort((left, right) => right.source.length - left.source.length);
  let result = expression;
  for (const replacement of replacements) {
    result = replaceFlowchartIdentifier(result, replacement.source, replacement.target);
  }
  return result;
}

function replaceFlowchartIdentifier(value: string, source: string, target: string): string {
  const pattern = new RegExp(`(^|[^A-Za-z0-9_])(${escapeRegExp(source)})(?=$|[^A-Za-z0-9_])`, "g");
  return value.replace(pattern, `$1${target}`);
}

function escapeRegExp(value: string): string {
  return value.replace(/[\\^$.*+?()[\]{}|]/g, "\\$&");
}

function currentFlowchartSymbolDocument(
  context: FlowchartContext,
): AspFlowchartSymbolDocument | undefined {
  return context.symbols.documents.find((document) => document.uri === context.symbols.currentUri);
}

function resolveFlowchartReference(
  context: FlowchartSymbolContext,
  reference: AspFlowchartSymbolReference,
): AspFlowchartResolvedDeclaration | undefined {
  if (reference.resolvedId) {
    return context.declarationsById.get(reference.resolvedId);
  }
  if (reference.memberName) {
    return resolveFlowchartMember(context, reference.baseName, reference.memberName);
  }
  return resolveFlowchartDeclarationByName(
    context,
    reference.normalizedName,
    reference.expectedKinds,
  );
}

function resolveFlowchartCallSite(
  context: FlowchartSymbolContext,
  callSite: AspFlowchartCallSite,
): AspFlowchartResolvedDeclaration | undefined {
  if (callSite.resolvedId) {
    return context.declarationsById.get(callSite.resolvedId);
  }
  if (callSite.memberName) {
    return resolveFlowchartMember(context, callSite.receiverName, callSite.memberName);
  }
  return resolveFlowchartDeclarationByName(
    context,
    callSite.normalizedName,
    expectedFlowchartKindsForCallSite(callSite),
  );
}

function resolveFlowchartDeclarationByName(
  context: FlowchartSymbolContext,
  normalizedName: string,
  expectedKinds: string[] | undefined,
): AspFlowchartResolvedDeclaration | undefined {
  return (context.declarationsByName.get(normalizedName) ?? []).find(
    (declaration) =>
      (!expectedKinds || expectedKinds.includes(declaration.kind)) &&
      (declaration.uri === context.currentUri || declaration.bindingScope !== "local"),
  );
}

function resolveFlowchartMember(
  context: FlowchartSymbolContext,
  receiverName: string | undefined,
  memberName: string,
): AspFlowchartResolvedDeclaration | undefined {
  if (!receiverName) {
    return undefined;
  }
  const receiverKey = receiverName.toLowerCase();
  const direct = context.membersByOwnerAndName.get(
    `${receiverKey}\0${memberName.toLowerCase()}`,
  )?.[0];
  if (direct) {
    return direct;
  }
  const receiverDeclaration = resolveFlowchartDeclarationByName(context, receiverKey, [
    "variable",
    "field",
    "parameter",
  ]);
  const owner = receiverDeclaration?.typeName ?? receiverName;
  return context.membersByOwnerAndName.get(
    `${owner.toLowerCase()}\0${memberName.toLowerCase()}`,
  )?.[0];
}

function expectedFlowchartKindsForCallSite(callSite: AspFlowchartCallSite): string[] | undefined {
  if (callSite.callKind === "constructor") {
    return ["class"];
  }
  if (callSite.callKind === "function") {
    return ["function"];
  }
  if (callSite.callKind === "procedure") {
    return ["function", "sub", "method", "property"];
  }
  if (callSite.callKind === "unknown") {
    return ["function", "sub", "class", "method", "property"];
  }
  return undefined;
}

function flowchartLink(
  context: FlowchartContext,
  label: string,
  role: AspFlowchartNodeLink["role"],
  declaration: AspFlowchartResolvedDeclaration,
): AspFlowchartNodeLink {
  const displayLabel = flowchartSymbolLabel(label, declaration, context);
  return {
    id: `${role}:${declaration.id}:${label}`,
    label: displayLabel,
    role,
    symbolKind: flowchartSymbolKind(declaration),
    target: {
      uri: declaration.uri,
      range: declaration.range,
      nameRange: declaration.nameRange,
    },
  };
}

function flowchartSymbolKind(declaration: AspFlowchartResolvedDeclaration): string {
  return declaration.unresolvedGlobal === true ? "unresolvedGlobalVariable" : declaration.kind;
}

function flowchartDeclarationListLabel(
  declarations: AspFlowchartResolvedDeclaration[],
  context: FlowchartContext,
): string {
  return declarations
    .map((declaration) => flowchartSymbolLabel(declaration.name, declaration, context))
    .join(context.locale === "ja" ? "、" : ", ");
}

function flowchartSymbolLabel(
  name: string,
  declaration: AspFlowchartResolvedDeclaration | undefined,
  context: FlowchartContext,
): string {
  if (!declaration) {
    return name;
  }
  return `${flowchartDeclarationKindLabel(declaration, context)} ${name}`;
}

function flowchartDeclarationKindLabel(
  declaration: Pick<
    AspFlowchartResolvedDeclaration,
    "kind" | "bindingScope" | "memberOf" | "procedureKind"
  >,
  context: FlowchartContext,
): string {
  const scope = flowchartScopeLabel(declaration.bindingScope, context);
  if (context.locale === "ja") {
    switch (declaration.kind) {
      case "variable":
        return `${scope}変数`;
      case "constant":
        return `${scope}定数`;
      case "parameter":
        return "引数";
      case "field":
        return "フィールド";
      case "function":
        return "関数";
      case "sub":
        return "Sub";
      case "class":
        return "クラス";
      case "method":
        return declaration.procedureKind === "function"
          ? "関数メソッド"
          : declaration.procedureKind === "sub"
            ? "Subメソッド"
            : "メソッド";
      case "property":
        return "プロパティ";
      default:
        return declaration.kind || "シンボル";
    }
  }
  switch (declaration.kind) {
    case "variable":
      return `${scope}variable`;
    case "constant":
      return `${scope}constant`;
    case "parameter":
      return "parameter";
    case "field":
      return "field";
    case "function":
      return "function";
    case "sub":
      return "Sub";
    case "class":
      return "class";
    case "method":
      return declaration.procedureKind === "function"
        ? "function method"
        : declaration.procedureKind === "sub"
          ? "Sub method"
          : "method";
    case "property":
      return "property";
    default:
      return declaration.kind || "symbol";
  }
}

function flowchartScopeLabel(scope: string | undefined, context: FlowchartContext): string {
  if (context.locale === "ja") {
    if (scope === "global") {
      return "グローバル";
    }
    if (scope === "local") {
      return "ローカル";
    }
    return "";
  }
  if (scope === "global") {
    return "global ";
  }
  if (scope === "local") {
    return "local ";
  }
  return "";
}

function flowchartCallSiteLabel(callSite: AspFlowchartCallSite): string {
  return callSite.memberName && callSite.receiverName
    ? `${callSite.receiverName}.${callSite.memberName}`
    : callSite.name;
}

function dedupeFlowchartLinks(links: AspFlowchartNodeLink[]): AspFlowchartNodeLink[] {
  const seen = new Set<string>();
  const result: AspFlowchartNodeLink[] = [];
  for (const link of links) {
    const key = `${link.role}:${link.label}:${link.target.uri}:${JSON.stringify(link.target.nameRange ?? link.target.range)}`;
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    result.push(link);
  }
  return result;
}

function rangeContainsRange(outer: Range, inner: Range): boolean {
  return (
    positionBeforeOrEqual(outer.start, inner.start) && positionBeforeOrEqual(inner.end, outer.end)
  );
}

function positionBeforeOrEqual(
  left: { line: number; character: number },
  right: { line: number; character: number },
): boolean {
  return left.line < right.line || (left.line === right.line && left.character <= right.character);
}

function createFlowchartSymbolContext(
  currentUri: string,
  documents: AspFlowchartSymbolDocument[],
): FlowchartSymbolContext {
  const declarationsById = new Map<string, AspFlowchartResolvedDeclaration>();
  const declarationsByName = new Map<string, AspFlowchartResolvedDeclaration[]>();
  const membersByOwnerAndName = new Map<string, AspFlowchartResolvedDeclaration[]>();
  for (const document of documents) {
    for (const declaration of document.declarations) {
      const resolved = { ...declaration, uri: document.uri };
      declarationsById.set(declaration.id, resolved);
      pushMapItem(declarationsByName, declaration.normalizedName, resolved);
      if (declaration.memberOf) {
        pushMapItem(
          membersByOwnerAndName,
          `${declaration.memberOf.toLowerCase()}\0${declaration.normalizedName}`,
          resolved,
        );
      }
    }
  }
  return { currentUri, documents, declarationsById, declarationsByName, membersByOwnerAndName };
}

function pushMapItem<K, V>(map: Map<K, V[]>, key: K, value: V): void {
  const existing = map.get(key);
  if (existing) {
    existing.push(value);
  } else {
    map.set(key, [value]);
  }
}

function flowchartText(locale: AspLocale): FlowchartText {
  if (locale === "ja") {
    return {
      topLevel: "トップレベル",
      start: "開始",
      end: "終了",
      else: "それ以外",
      yes: "はい",
      no: "いいえ",
      repeat: "繰り返し",
      exit: "終了",
      ifCondition: (condition) => `${condition}を判定`,
      elseifCondition: (condition) => `${condition}を追加判定`,
      selectCase: (expression) => `${expression}で分岐`,
      caseBranch: (expression) => `${expression}の場合`,
      forRange: (variable, start, end, step) =>
        `${variable}を${start}から${end}まで${step ? ` ${step}ずつ` : ""}繰り返し`,
      forEach: (variable, collection) => `${collection}の各${variable}で繰り返し`,
      loopWhile: (condition) => `${condition}の間繰り返し`,
      loopUntil: (condition) => `${condition}になるまで繰り返し`,
      repeatLoop: (statement) => `${statement}を繰り返し`,
      call: (name, args) => `${name}${args ? `(${args})` : ""}の呼び出し`,
      assign: (target, value) => `${target}に${value}を代入`,
      declare: (symbols) => `${symbols}を宣言`,
      exitStatement: (kind) => `${kind || "処理"}を終了`,
      statement: (value) => `${value}を実行`,
      symbolRole: (role) => flowchartRoleTextJa[role] ?? role,
    };
  }
  return {
    topLevel: "Top level",
    start: "Start",
    end: "End",
    else: "Otherwise",
    yes: "Yes",
    no: "No",
    repeat: "Repeat",
    exit: "Exit",
    ifCondition: (condition) => `Check ${condition}`,
    elseifCondition: (condition) => `Otherwise check ${condition}`,
    selectCase: (expression) => `Branch by ${expression}`,
    caseBranch: (expression) => `When ${expression}`,
    forRange: (variable, start, end, step) =>
      `Repeat ${variable} from ${start} to ${end}${step ? ` by ${step}` : ""}`,
    forEach: (variable, collection) => `Repeat for each ${variable} in ${collection}`,
    loopWhile: (condition) => `Repeat while ${condition}`,
    loopUntil: (condition) => `Repeat until ${condition}`,
    repeatLoop: (statement) => `Repeat ${statement}`,
    call: (name, args) => `Call ${name}${args ? `(${args})` : ""}`,
    assign: (target, value) => `Assign ${value} to ${target}`,
    declare: (symbols) => `Declare ${symbols}`,
    exitStatement: (kind) => `Exit ${kind || "block"}`,
    statement: (value) => `Run ${value}`,
    symbolRole: (role) => flowchartRoleTextEn[role] ?? role,
  };
}

const flowchartRoleTextEn: Record<string, string> = {
  read: "Read",
  write: "Write",
  call: "Call",
  new: "Create",
  member: "Member",
  definition: "Definition",
  unknown: "Reference",
};

const flowchartRoleTextJa: Record<string, string> = {
  read: "参照",
  write: "代入",
  call: "呼び出し",
  new: "作成",
  member: "メンバー",
  definition: "定義",
  unknown: "参照",
};

function normalizeFlowchartName(value: string): string {
  return value.replace(/\s+/g, "").toLowerCase();
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

function tokenTextIndex(tokens: VbToken[], text: string, startIndex = 0): number {
  return tokens.findIndex((token, index) => index >= startIndex && token.text === text);
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
