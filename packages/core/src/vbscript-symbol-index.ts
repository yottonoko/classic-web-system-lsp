import type { Range } from "vscode-languageserver-types";
import type { AspInclude, AspRegion, AspServerObject, AspSettings, VbToken } from "./types";
import { normalizeScriptLanguage, parseAttributes, scanHtmlAndAsp } from "./asp-scanner";
import { offsetAt } from "./position";
import { tokenizeVbscript, unquoteVbString } from "./vbscript-cst";
import builtinCatalogData from "./vbscript-builtin-catalog.json";

export type VbIndexedDeclarationKind =
  | "class"
  | "function"
  | "sub"
  | "method"
  | "property"
  | "constant"
  | "variable"
  | "field"
  | "parameter";

export type VbBindingScope = "global" | "local" | "unknown";

export type VbIndexedReferenceRole = "read" | "write" | "call" | "new" | "member" | "unknown";

export type VbIndexedCallKind = "procedure" | "function" | "constructor" | "member" | "unknown";

export type VbIndexedProcedureKind = "sub" | "function" | "property";

export interface VbSymbolIndexOptions {
  includeReferences?: boolean;
  includeParameters?: boolean;
  includeImplicitVariables?: boolean;
  resolveMembers?: "none" | "simple-new";
}

export interface VbIndexedParameter {
  name: string;
  normalizedName: string;
  range: Range;
  declarationId: string;
}

export interface VbIndexedDeclaration {
  id: string;
  name: string;
  normalizedName: string;
  kind: VbIndexedDeclarationKind;
  range: Range;
  nameRange: Range;
  sourceRange?: Range;
  scopeId?: string;
  parentId?: string;
  memberOf?: string;
  visibility?: "public" | "private";
  bindingScope?: VbBindingScope;
  procedureKind?: VbIndexedProcedureKind;
  parameters?: VbIndexedParameter[];
  implicit?: boolean;
  typeName?: string;
  arrayKind?: "fixed" | "dynamic";
  arrayDimensions?: string[];
}

export interface VbIndexedReference {
  name: string;
  normalizedName: string;
  range: Range;
  scopeId?: string;
  resolvedId?: string;
  role: VbIndexedReferenceRole;
  bindingScope?: VbBindingScope;
  expectedKinds?: VbIndexedDeclarationKind[];
  deferredKey?: string;
  baseName?: string;
  memberName?: string;
}

export interface VbIndexedCallSite {
  name: string;
  normalizedName: string;
  range: Range;
  scopeId?: string;
  receiverName?: string;
  memberName?: string;
  callKind: VbIndexedCallKind;
  argumentCount?: number;
  resolvedId?: string;
  deferredKey?: string;
}

export interface VbDeferredExternalReference {
  key: string;
  name: string;
  normalizedName: string;
  range: Range;
  scopeId?: string;
  expectedKinds: VbIndexedDeclarationKind[];
  role: VbIndexedReferenceRole;
  bindingScope?: VbBindingScope;
  receiverName?: string;
  memberName?: string;
  localResolutionId?: string;
  reason: "unresolved-local" | "member-access" | "ambiguous" | "include-candidate";
}

export interface VbSymbolIndexStats {
  regions: number;
  tokens: number;
  declarations: number;
  references: number;
  callSites: number;
  deferredExternalRefs: number;
}

export interface VbSymbolIndex {
  uri: string;
  declarations: VbIndexedDeclaration[];
  references: VbIndexedReference[];
  callSites: VbIndexedCallSite[];
  deferredExternalRefs: VbDeferredExternalReference[];
  includeRefs: AspInclude[];
  stats: VbSymbolIndexStats;
}

interface ScopeFrame {
  id: string;
  kind: "global" | "class" | "procedure";
  start: number;
  end: number;
  procedureKind?: VbIndexedProcedureKind;
  declarationId?: string;
  name?: string;
  normalizedName?: string;
  parentId?: string;
  memberOf?: string;
}

interface SymbolIndexBuildState {
  uri: string;
  text: string;
  options: Required<VbSymbolIndexOptions>;
  declarations: VbIndexedDeclaration[];
  declarationNameKeys: Set<string>;
  hasOptionExplicit: boolean;
  lineStarts: number[];
  scopes: ScopeFrame[];
  stack: ScopeFrame[];
  tokens: VbToken[];
}

interface VbRegionIndexInput {
  regions: AspRegion[];
  includeRefs: AspInclude[];
  serverObjects: AspServerObject[];
}

interface DeclarationNameEntry {
  token: VbToken;
  arrayKind?: "fixed" | "dynamic";
  arrayDimensions?: string[];
}

interface DeclarationLookup {
  byId: Map<string, VbIndexedDeclaration>;
  byScopeName: Map<string, VbIndexedDeclaration[]>;
  byParentName: Map<string, VbIndexedDeclaration[]>;
}

interface IndexedReferenceToken {
  reference: VbIndexedReference;
  token: VbToken;
  tokenIndex: number;
}

interface BuiltinIndexObjectSpec {
  typeName: string;
  members: Array<{ name: string; type?: string }>;
}

const builtinClassicAspObjects = builtinCatalogData.classicAspObjects as Record<
  string,
  BuiltinIndexObjectSpec
>;
const builtinExternalObjects = builtinCatalogData.externalObjects as Record<
  string,
  BuiltinIndexObjectSpec
>;

const defaultOptions: Required<VbSymbolIndexOptions> = {
  includeReferences: true,
  includeParameters: true,
  includeImplicitVariables: false,
  resolveMembers: "none",
};

const globalScopeId = "global";
const classExpectedKinds: VbIndexedDeclarationKind[] = ["class"];
const callableExpectedKinds: VbIndexedDeclarationKind[] = ["function", "sub", "method", "property"];
const memberExpectedKinds: VbIndexedDeclarationKind[] = [
  "method",
  "property",
  "field",
  "variable",
  "constant",
];
const writableExpectedKinds: VbIndexedDeclarationKind[] = ["variable"];
const valueExpectedKinds: VbIndexedDeclarationKind[] = ["variable", "constant"];
const implicitVariableExcludedNames = new Set(
  [
    "application",
    "asperror",
    "empty",
    "err",
    "false",
    "me",
    "nothing",
    "null",
    "request",
    "response",
    "server",
    "session",
    "true",
    ...Object.keys(builtinCatalogData.classicAspObjects),
    ...Object.keys(builtinCatalogData.externalObjects),
    ...builtinCatalogData.constants.map((item) => item.label),
    ...builtinCatalogData.functions.map((item) => item.label),
    ...builtinCatalogData.runtimeEvents.map((item) => item.label),
  ].map((name) => name.toLowerCase()),
);

export function extractVbscriptSymbolIndex(
  uri: string,
  text: string,
  settings: AspSettings = {},
  options: VbSymbolIndexOptions = {},
): VbSymbolIndex {
  const resolvedOptions = { ...defaultOptions, ...options };
  const input = collectVbscriptIndexInput(text, settings);
  const tokens = tokensForRegions(text, input.regions);
  const state: SymbolIndexBuildState = {
    uri,
    text,
    options: resolvedOptions,
    declarations: [],
    declarationNameKeys: new Set(),
    hasOptionExplicit: false,
    lineStarts: lineStartsForText(text),
    scopes: [
      {
        id: globalScopeId,
        kind: "global",
        start: 0,
        end: text.length,
      },
    ],
    stack: [
      {
        id: globalScopeId,
        kind: "global",
        start: 0,
        end: text.length,
      },
    ],
    tokens,
  };
  addServerObjectDeclarations(state, input.serverObjects);
  collectDeclarations(state);
  applySimpleAssignmentTypes(state);
  const referenceResult = resolvedOptions.includeReferences
    ? collectReferences(state)
    : { references: [], callSites: [], deferredExternalRefs: [] };
  return {
    uri,
    declarations: state.declarations,
    references: referenceResult.references,
    callSites: referenceResult.callSites,
    deferredExternalRefs: referenceResult.deferredExternalRefs,
    includeRefs: input.includeRefs,
    stats: {
      regions: input.regions.length,
      tokens: tokens.filter((token) => !isBoundaryToken(token)).length,
      declarations: state.declarations.length,
      references: referenceResult.references.length,
      callSites: referenceResult.callSites.length,
      deferredExternalRefs: referenceResult.deferredExternalRefs.length,
    },
  };
}

function collectVbscriptIndexInput(text: string, settings: AspSettings): VbRegionIndexInput {
  const scan = scanHtmlAndAsp(text, [], settings);
  const directiveLanguage = scan.inlineRegions
    .filter((region) => region.kind === "asp-directive")
    .map((region) => directiveLanguageFromRegion(text, region))
    .find((language): language is string => language !== undefined);
  const defaultLanguage = normalizeScriptLanguage(
    directiveLanguage ?? settings.defaultLanguage ?? "VBScript",
  );
  const scriptRegions = scan.tagRegions.map((region): AspRegion => {
    if (region.kind !== "server-script") {
      return region;
    }
    return {
      ...region,
      language:
        normalizeScriptLanguage(
          String(region.attributes?.language ?? region.attributes?.type ?? defaultLanguage),
        ).toLowerCase() === "jscript"
          ? "jscript"
          : "vbscript",
    };
  });
  const candidates = [...scan.inlineRegions, ...scriptRegions];
  const regions = candidates
    .filter((region) => region.end > region.start)
    .map((region): AspRegion => {
      const language =
        region.language === "vbscript" &&
        defaultLanguage === "JScript" &&
        (region.kind === "asp-block" || region.kind === "asp-expression")
          ? "jscript"
          : region.language;
      return { ...region, language };
    })
    .filter(
      (region) =>
        region.language === "vbscript" &&
        region.kind !== "asp-directive" &&
        region.contentEnd > region.contentStart,
    )
    .sort(
      (left, right) => left.contentStart - right.contentStart || left.contentEnd - right.contentEnd,
    );
  return { regions, includeRefs: scan.includes, serverObjects: scan.serverObjects };
}

function directiveLanguageFromRegion(text: string, region: AspRegion): string | undefined {
  const raw = text.slice(region.contentStart, region.contentEnd).trim();
  const normalized = raw.startsWith("@") ? raw.slice(1).trim() : raw;
  const [first = "Page", ...rest] = normalized.split(/\s+/);
  const attributeText = first.includes("=") ? normalized : rest.join(" ");
  const attributes = parseAttributes(attributeText);
  return typeof attributes.language === "string"
    ? attributes.language
    : typeof attributes.LANGUAGE === "string"
      ? attributes.LANGUAGE
      : undefined;
}

function tokensForRegions(text: string, regions: AspRegion[]): VbToken[] {
  const tokens: VbToken[] = [];
  for (const region of regions) {
    tokens.push(
      ...tokenizeVbscript(text.slice(region.contentStart, region.contentEnd), region.contentStart),
    );
    tokens.push({
      kind: "newline",
      start: region.contentEnd,
      end: region.contentEnd,
      text: "\n",
    });
  }
  return tokens.filter((token) => token.kind !== "whitespace" && token.kind !== "comment");
}

function addServerObjectDeclarations(
  state: SymbolIndexBuildState,
  serverObjects: AspServerObject[],
): void {
  for (const serverObject of serverObjects) {
    if (!isVbServerObjectIdentifier(serverObject.id)) {
      continue;
    }
    const start = offsetAt(state.text, serverObject.idRange.start);
    const end = offsetAt(state.text, serverObject.idRange.end);
    const nameToken: VbToken = {
      kind: "identifier",
      start,
      end,
      text: serverObject.id,
    };
    addDeclaration(state, {
      kind: "variable",
      nameToken,
      start,
      end,
      scopeId: globalScopeId,
      bindingScope: "global",
      sourceRange: serverObject.range,
      typeName: serverObject.progId
        ? (canonicalBuiltinObjectTypeName(serverObject.progId) ?? serverObject.progId)
        : undefined,
    });
  }
}

function isVbServerObjectIdentifier(name: string): boolean {
  return /^[A-Za-z_][A-Za-z0-9_]*$/.test(name);
}

function collectDeclarations(state: SymbolIndexBuildState): void {
  const tokens = state.tokens;
  for (let index = 0; index < tokens.length; index += 1) {
    const token = tokens[index];
    if (!isStatementStart(tokens, index) || isBoundaryToken(token)) {
      continue;
    }
    const first = lower(token);
    const second = lower(tokens[index + 1]);
    if (first === "option" && second === "explicit") {
      state.hasOptionExplicit = true;
      continue;
    }
    if (first === "end") {
      closeScope(state, second, token);
      continue;
    }
    if (first === "class" && tokens[index + 1]?.kind === "identifier") {
      openClassScope(state, token, tokens[index + 1]);
      continue;
    }
    const visibility =
      first === "public" || first === "private" ? (first as "public" | "private") : undefined;
    const declarationStart = visibility ? second : first;
    const declarationOffset = visibility ? 1 : 0;
    if (declarationStart === "sub" || declarationStart === "function") {
      const nameToken = tokens[index + declarationOffset + 1];
      if (nameToken?.kind === "identifier") {
        openProcedureScope(
          state,
          declarationStart,
          token,
          nameToken,
          index + declarationOffset + 2,
          visibility,
        );
      }
      continue;
    }
    if (declarationStart === "property") {
      const accessor = lower(tokens[index + declarationOffset + 1]);
      const nameToken = tokens[index + declarationOffset + 2];
      if (
        (accessor === "get" || accessor === "let" || accessor === "set") &&
        nameToken?.kind === "identifier"
      ) {
        openProcedureScope(
          state,
          "property",
          token,
          nameToken,
          index + declarationOffset + 3,
          visibility,
        );
      }
      continue;
    }
    if (first === "dim" || first === "redim") {
      const startIndex = first === "redim" && second === "preserve" ? index + 2 : index + 1;
      addVariableDeclarations(state, "variable", token, startIndex, undefined, first === "redim");
      continue;
    }
    if (first === "const") {
      addVariableDeclarations(state, "constant", token, index + 1, undefined);
      continue;
    }
    if (visibility) {
      if (second === "const") {
        addVariableDeclarations(state, "constant", token, index + 2, visibility);
      } else {
        addVariableDeclarations(state, "variable", token, index + 1, visibility);
      }
    }
  }
  const end = tokens.at(-1)?.end ?? state.text.length;
  for (const scope of state.stack) {
    scope.end = Math.max(scope.end, end);
    updateProcedureDeclarationSourceRange(state, scope);
  }
}

function openClassScope(
  state: SymbolIndexBuildState,
  startToken: VbToken,
  nameToken: VbToken,
): void {
  const declaration = addDeclaration(state, {
    kind: "class",
    nameToken,
    start: startToken.start,
    end: nameToken.end,
    scopeId: currentScope(state).id,
  });
  const scope: ScopeFrame = {
    id: declaration.id,
    kind: "class",
    start: startToken.start,
    end: state.text.length,
    declarationId: declaration.id,
    name: declaration.name,
    normalizedName: declaration.normalizedName,
    parentId: currentScope(state).declarationId,
  };
  state.scopes.push(scope);
  state.stack.push(scope);
}

function openProcedureScope(
  state: SymbolIndexBuildState,
  procedureKind: VbIndexedProcedureKind,
  startToken: VbToken,
  nameToken: VbToken,
  parameterStartIndex: number,
  visibility?: "public" | "private",
): void {
  const classScope = [...state.stack].reverse().find((scope) => scope.kind === "class");
  const kind: VbIndexedDeclarationKind =
    procedureKind === "property" ? "property" : classScope ? "method" : procedureKind;
  const parameters = state.options.includeParameters
    ? collectParameterTokens(state.tokens, parameterStartIndex).map((token) => ({
        token,
        id: declarationId(state.uri, "parameter", token),
      }))
    : [];
  const declaration = addDeclaration(state, {
    kind,
    nameToken,
    start: startToken.start,
    end: nameToken.end,
    scopeId: classScope?.id ?? globalScopeId,
    parentId: classScope?.declarationId,
    memberOf: classScope?.name,
    visibility,
    procedureKind,
    parameters: parameters.map((parameter) => ({
      name: parameter.token.text,
      normalizedName: normalizeName(parameter.token.text),
      range: rangeAt(state, parameter.token.start, parameter.token.end),
      declarationId: parameter.id,
    })),
  });
  const scope: ScopeFrame = {
    id: declaration.id,
    kind: "procedure",
    start: startToken.start,
    end: state.text.length,
    procedureKind,
    declarationId: declaration.id,
    name: declaration.name,
    normalizedName: declaration.normalizedName,
    parentId: classScope?.declarationId,
    memberOf: classScope?.name,
  };
  state.scopes.push(scope);
  state.stack.push(scope);
  for (const parameter of parameters) {
    addDeclaration(state, {
      kind: "parameter",
      nameToken: parameter.token,
      start: parameter.token.start,
      end: parameter.token.end,
      scopeId: scope.id,
      bindingScope: "local",
    });
  }
}

function addVariableDeclarations(
  state: SymbolIndexBuildState,
  baseKind: "variable" | "constant",
  startToken: VbToken,
  startIndex: number,
  visibility?: "public" | "private",
  redim = false,
): void {
  const current = currentScope(state);
  const classScope = current.kind === "class" ? current : undefined;
  const procedureScope = current.kind === "procedure" ? current : undefined;
  const kind: VbIndexedDeclarationKind = baseKind === "variable" && classScope ? "field" : baseKind;
  const bindingScope: VbBindingScope | undefined =
    kind === "field" || classScope ? undefined : procedureScope ? "local" : "global";
  for (const entry of declarationNameEntries(state.tokens, startIndex)) {
    addDeclaration(state, {
      kind,
      nameToken: entry.token,
      start: startToken.start,
      end: entry.token.end,
      scopeId: classScope?.id ?? procedureScope?.id ?? globalScopeId,
      parentId: classScope?.declarationId,
      memberOf: classScope?.name,
      visibility,
      bindingScope,
      typeName: entry.arrayKind ? "Array" : undefined,
      arrayKind: redim && entry.arrayKind ? "dynamic" : entry.arrayKind,
      arrayDimensions: entry.arrayDimensions,
    });
  }
}

function addDeclaration(
  state: SymbolIndexBuildState,
  input: {
    kind: VbIndexedDeclarationKind;
    nameToken: VbToken;
    start: number;
    end: number;
    scopeId?: string;
    parentId?: string;
    memberOf?: string;
    visibility?: "public" | "private";
    bindingScope?: VbBindingScope;
    procedureKind?: VbIndexedProcedureKind;
    parameters?: VbIndexedParameter[];
    implicit?: boolean;
    sourceRange?: Range;
    typeName?: string;
    arrayKind?: "fixed" | "dynamic";
    arrayDimensions?: string[];
  },
): VbIndexedDeclaration {
  const declaration: VbIndexedDeclaration = {
    id: declarationId(state.uri, input.kind, input.nameToken),
    name: input.nameToken.text,
    normalizedName: normalizeName(input.nameToken.text),
    kind: input.kind,
    range: rangeAt(state, input.start, input.end),
    nameRange: rangeAt(state, input.nameToken.start, input.nameToken.end),
    scopeId: input.scopeId,
    parentId: input.parentId,
    memberOf: input.memberOf,
    visibility: input.visibility,
    bindingScope: input.bindingScope,
    procedureKind: input.procedureKind,
    parameters: input.parameters,
    implicit: input.implicit,
    sourceRange: input.sourceRange,
    typeName: input.typeName,
    arrayKind: input.arrayKind,
    arrayDimensions: input.arrayDimensions,
  };
  state.declarations.push(declaration);
  state.declarationNameKeys.add(tokenKey(input.nameToken));
  return declaration;
}

function applySimpleAssignmentTypes(state: SymbolIndexBuildState): void {
  const lookup = declarationLookup(state.declarations);
  for (let index = 0; index < state.tokens.length; index += 1) {
    const token = state.tokens[index];
    if (!isStatementStart(state.tokens, index) || isBoundaryToken(token)) {
      continue;
    }
    const endIndex = statementEndIndex(state.tokens, index);
    const targetIndex = simpleAssignmentTargetIndex(state.tokens, index, endIndex);
    if (targetIndex === -1) {
      continue;
    }
    const target = state.tokens[targetIndex];
    const equalsIndex = nextTokenIndexInStatement(state.tokens, targetIndex + 1, endIndex, "=");
    if (target.kind !== "identifier" || equalsIndex === -1) {
      continue;
    }
    const typeName = simpleAssignmentTypeName(state, lookup, equalsIndex + 1, endIndex);
    if (!typeName) {
      continue;
    }
    const declaration = resolveDeclaration(state, lookup, target, "write", undefined);
    if (declaration && !declaration.typeName) {
      declaration.typeName = typeName;
    }
  }
}

function simpleAssignmentTargetIndex(
  tokens: VbToken[],
  startIndex: number,
  endIndex: number,
): number {
  const first = lower(tokens[startIndex]);
  if (first === "set" || first === "let") {
    return nextTokenIndexInStatement(tokens, startIndex + 1, endIndex);
  }
  return tokens[startIndex]?.kind === "identifier" ? startIndex : -1;
}

function simpleAssignmentTypeName(
  state: SymbolIndexBuildState,
  lookup: DeclarationLookup,
  startIndex: number,
  endIndex: number,
): string | undefined {
  const firstIndex = nextTokenIndexInStatement(state.tokens, startIndex, endIndex);
  if (firstIndex === -1) {
    return undefined;
  }
  const first = state.tokens[firstIndex];
  if (lower(first) === "new") {
    const classToken =
      state.tokens[nextTokenIndexInStatement(state.tokens, firstIndex + 1, endIndex)];
    return classToken?.kind === "identifier"
      ? canonicalBuiltinObjectTypeName(classToken.text)
      : undefined;
  }
  const createObjectTypeName = createObjectAssignmentTypeName(state.tokens, firstIndex, endIndex);
  if (createObjectTypeName) {
    return createObjectTypeName;
  }
  const dotIndex = nextTokenIndexInStatement(state.tokens, firstIndex + 1, endIndex, ".");
  if (first.kind !== "identifier" || dotIndex === -1) {
    return undefined;
  }
  const memberIndex = nextTokenIndexInStatement(state.tokens, dotIndex + 1, endIndex);
  const member = state.tokens[memberIndex];
  if (member?.kind !== "identifier") {
    return undefined;
  }
  const receiver = resolveDeclaration(state, lookup, first, "read", undefined);
  return receiver?.typeName ? builtinMemberTypeName(receiver.typeName, member.text) : undefined;
}

function createObjectAssignmentTypeName(
  tokens: VbToken[],
  startIndex: number,
  endIndex: number,
): string | undefined {
  const first = lower(tokens[startIndex]);
  const createObjectIndex =
    first === "createobject"
      ? startIndex
      : first === "server" &&
          tokens[nextTokenIndexInStatement(tokens, startIndex + 1, endIndex)]?.text === "." &&
          lower(tokens[nextTokenIndexInStatement(tokens, startIndex + 2, endIndex)]) ===
            "createobject"
        ? nextTokenIndexInStatement(tokens, startIndex + 2, endIndex)
        : -1;
  if (createObjectIndex === -1) {
    return undefined;
  }
  for (let index = createObjectIndex + 1; index <= endIndex; index += 1) {
    const token = tokens[index];
    if (token?.kind === "string") {
      const progId = token.value ?? unquoteVbString(token.text);
      return canonicalBuiltinObjectTypeName(progId) ?? progId;
    }
  }
  return undefined;
}

function builtinMemberTypeName(ownerTypeName: string, memberName: string): string | undefined {
  const objectSpec = builtinObjectSpecForTypeName(ownerTypeName);
  const member = objectSpec?.members.find(
    (candidate) => candidate.name.toLowerCase() === memberName.toLowerCase(),
  );
  return member?.type ? (canonicalBuiltinObjectTypeName(member.type) ?? member.type) : undefined;
}

function builtinObjectSpecForTypeName(typeName: string): BuiltinIndexObjectSpec | undefined {
  const lowerName = typeName.toLowerCase();
  const objects = [
    ...Object.values(builtinClassicAspObjects),
    ...Object.values(builtinExternalObjects),
  ];
  return objects.find((candidate) => candidate.typeName.toLowerCase() === lowerName);
}

function canonicalBuiltinObjectTypeName(name: string): string | undefined {
  const lowerName = name.toLowerCase();
  return (
    builtinObjectSpecForTypeName(name)?.typeName ??
    builtinClassicAspObjects[lowerName]?.typeName ??
    builtinExternalObjects[lowerName]?.typeName
  );
}

function collectReferences(state: SymbolIndexBuildState): {
  references: VbIndexedReference[];
  callSites: VbIndexedCallSite[];
  deferredExternalRefs: VbDeferredExternalReference[];
} {
  const references: VbIndexedReference[] = [];
  const callSites: VbIndexedCallSite[] = [];
  const deferredExternalRefs: VbDeferredExternalReference[] = [];
  const lookup = declarationLookup(state.declarations);
  const collectImplicitReferences =
    state.options.includeImplicitVariables && !state.hasOptionExplicit;
  const deferredReferenceTokens: IndexedReferenceToken[] = [];
  const implicitWriteReferenceTokens: IndexedReferenceToken[] = [];
  for (let index = 0; index < state.tokens.length; index += 1) {
    const token = state.tokens[index];
    if (token.kind !== "identifier" || state.declarationNameKeys.has(tokenKey(token))) {
      continue;
    }
    if (isTypeAnnotationIdentifier(state.tokens, index)) {
      continue;
    }
    let role = referenceRole(state.tokens, index);
    const baseName = memberBaseName(state.tokens, index);
    let resolved = resolveDeclaration(state, lookup, token, role, baseName);
    if (!resolved && role === "call") {
      const valueResolved = resolveDeclaration(state, lookup, token, "read", baseName);
      if (valueResolved && declarationCanBeIndexed(valueResolved.kind)) {
        role = "read";
        resolved = valueResolved;
      }
    }
    if (resolved && isFunctionReturnAssignmentReference(state, index, resolved)) {
      continue;
    }
    const expectedKinds = expectedKindsForReference(role, resolved);
    const scope =
      activeScopeAt(state, token.start, "procedure") ?? activeScopeAt(state, token.start, "class");
    const deferredKey = !resolved ? referenceKey(state.uri, token) : undefined;
    const reference: VbIndexedReference = {
      name: token.text,
      normalizedName: normalizeName(token.text),
      range: rangeAt(state, token.start, token.end),
      scopeId: scope?.id,
      resolvedId: resolved?.id,
      role,
      bindingScope: bindingScopeForReference(resolved, expectedKinds),
      expectedKinds,
      deferredKey,
      baseName,
      memberName: role === "member" ? token.text : undefined,
    };
    references.push(reference);
    if (role === "call" || role === "new" || role === "member") {
      callSites.push(callSiteFromReference(lookup, reference, role));
    }
    if (collectImplicitReferences && !resolved && expectedKinds) {
      const item = { reference, token, tokenIndex: index };
      deferredReferenceTokens.push(item);
      if (isImplicitVariableDeclarationCandidate(reference)) {
        implicitWriteReferenceTokens.push(item);
      }
    }
    if (!collectImplicitReferences && !resolved && expectedKinds) {
      deferredExternalRefs.push({
        key: deferredKey ?? referenceKey(state.uri, token),
        name: token.text,
        normalizedName: normalizeName(token.text),
        range: reference.range,
        scopeId: reference.scopeId,
        expectedKinds,
        role,
        bindingScope: reference.bindingScope,
        receiverName: baseName,
        memberName: role === "member" ? token.text : undefined,
        reason: role === "member" ? "member-access" : "include-candidate",
      });
    }
  }
  if (collectImplicitReferences) {
    const implicitNames = addImplicitVariableDeclarationsFromReferences(
      state,
      lookup,
      implicitWriteReferenceTokens,
    );
    resolveReferencesWithImplicitDeclarations(
      state,
      lookup,
      deferredReferenceTokens,
      implicitNames,
    );
    deferredExternalRefs.push(...deferredExternalRefsForReferences(state, deferredReferenceTokens));
  }
  return { references, callSites, deferredExternalRefs };
}

function addImplicitVariableDeclarationsFromReferences(
  state: SymbolIndexBuildState,
  lookup: DeclarationLookup,
  references: IndexedReferenceToken[],
): Set<string> {
  const implicitNames = new Set<string>();
  for (const item of references) {
    const { reference, token } = item;
    if (!isImplicitVariableDeclarationCandidate(reference)) {
      continue;
    }
    if (resolveDeclaration(state, lookup, token, reference.role, reference.baseName)) {
      continue;
    }
    const procedureScope = activeScopeAt(state, token.start, "procedure");
    const classScope = activeScopeAt(state, token.start, "class");
    const classField = !procedureScope && classScope;
    const declaration = addDeclaration(state, {
      kind: classField ? "field" : "variable",
      nameToken: token,
      start: token.start,
      end: token.end,
      scopeId: procedureScope?.id ?? classScope?.id ?? globalScopeId,
      parentId: classField ? classScope.declarationId : undefined,
      memberOf: classField ? classScope.name : undefined,
      bindingScope: classField ? undefined : procedureScope ? "local" : "global",
      implicit: true,
    });
    addDeclarationToLookup(lookup, declaration);
    implicitNames.add(declaration.normalizedName);
  }
  return implicitNames;
}

function isImplicitVariableDeclarationCandidate(reference: VbIndexedReference): boolean {
  return (
    reference.role === "write" &&
    !reference.resolvedId &&
    !reference.baseName &&
    reference.expectedKinds?.includes("variable") === true &&
    !implicitVariableExcludedNames.has(reference.normalizedName)
  );
}

function resolveReferencesWithImplicitDeclarations(
  state: SymbolIndexBuildState,
  lookup: DeclarationLookup,
  references: IndexedReferenceToken[],
  implicitNames: Set<string>,
): void {
  for (const item of references) {
    const { reference, token, tokenIndex } = item;
    if (
      reference.resolvedId ||
      reference.role === "member" ||
      !implicitNames.has(reference.normalizedName)
    ) {
      continue;
    }
    const resolved = resolveDeclaration(state, lookup, token, reference.role, reference.baseName);
    if (!resolved || isFunctionReturnAssignmentReference(state, tokenIndex, resolved)) {
      continue;
    }
    reference.resolvedId = resolved.id;
    reference.bindingScope = bindingScopeForReference(resolved, undefined);
    reference.expectedKinds = undefined;
    reference.deferredKey = undefined;
  }
}

function deferredExternalRefsForReferences(
  state: SymbolIndexBuildState,
  references: IndexedReferenceToken[],
): VbDeferredExternalReference[] {
  const deferred: VbDeferredExternalReference[] = [];
  for (const { reference, token } of references) {
    if (reference.resolvedId || !reference.expectedKinds) {
      continue;
    }
    deferred.push(
      deferredExternalRefFromReference(state, reference, token, reference.expectedKinds),
    );
  }
  return deferred;
}

function deferredExternalRefFromReference(
  state: SymbolIndexBuildState,
  reference: VbIndexedReference,
  token: VbToken,
  expectedKinds: VbIndexedDeclarationKind[],
): VbDeferredExternalReference {
  return {
    key: reference.deferredKey ?? referenceKey(state.uri, token),
    name: reference.name,
    normalizedName: reference.normalizedName,
    range: reference.range,
    scopeId: reference.scopeId,
    expectedKinds,
    role: reference.role,
    bindingScope: reference.bindingScope,
    receiverName: reference.baseName,
    memberName: reference.memberName,
    reason: reference.role === "member" ? "member-access" : "include-candidate",
  };
}

function declarationLookup(declarations: VbIndexedDeclaration[]): DeclarationLookup {
  const lookup: DeclarationLookup = {
    byId: new Map(),
    byScopeName: new Map(),
    byParentName: new Map(),
  };
  for (const declaration of declarations) {
    addDeclarationToLookup(lookup, declaration);
  }
  return lookup;
}

function addDeclarationToLookup(
  lookup: DeclarationLookup,
  declaration: VbIndexedDeclaration,
): void {
  lookup.byId.set(declaration.id, declaration);
  if (declaration.scopeId) {
    pushMapItem(
      lookup.byScopeName,
      declarationMapKey(declaration.scopeId, declaration.normalizedName),
      declaration,
    );
  }
  if (declaration.parentId) {
    pushMapItem(
      lookup.byParentName,
      declarationMapKey(declaration.parentId, declaration.normalizedName),
      declaration,
    );
  }
}

function pushMapItem<K, V>(map: Map<K, V[]>, key: K, value: V): void {
  const existing = map.get(key);
  if (existing) {
    existing.push(value);
  } else {
    map.set(key, [value]);
  }
}

function callSiteFromReference(
  lookup: DeclarationLookup,
  reference: VbIndexedReference,
  role: VbIndexedReferenceRole,
): VbIndexedCallSite {
  const resolved = reference.resolvedId ? lookup.byId.get(reference.resolvedId) : undefined;
  const callKind: VbIndexedCallKind =
    role === "new"
      ? "constructor"
      : role === "member"
        ? "member"
        : resolved?.kind === "function"
          ? "function"
          : resolved?.kind === "sub" || resolved?.kind === "method" || resolved?.kind === "property"
            ? "procedure"
            : "unknown";
  return {
    name: reference.name,
    normalizedName: reference.normalizedName,
    range: reference.range,
    scopeId: reference.scopeId,
    receiverName: reference.baseName,
    memberName: reference.memberName,
    callKind,
    resolvedId: reference.resolvedId,
    deferredKey: reference.deferredKey,
  };
}

function closeScope(
  state: SymbolIndexBuildState,
  endKind: string | undefined,
  endToken: VbToken,
): void {
  const targetKind =
    endKind === "class"
      ? "class"
      : endKind === "sub" || endKind === "function" || endKind === "property"
        ? "procedure"
        : undefined;
  if (!targetKind) {
    return;
  }
  const index = findLastIndex(state.stack, (scope) => scope.kind === targetKind);
  if (index <= 0) {
    return;
  }
  const [scope] = state.stack.splice(index, 1);
  scope.end = endToken.end;
  updateProcedureDeclarationSourceRange(state, scope);
}

function updateProcedureDeclarationSourceRange(
  state: SymbolIndexBuildState,
  scope: ScopeFrame,
): void {
  if (scope.kind !== "procedure" || !scope.declarationId) {
    return;
  }
  const declaration = state.declarations.find((item) => item.id === scope.declarationId);
  if (!declaration) {
    return;
  }
  declaration.sourceRange = rangeAt(state, scope.start, scope.end);
}

function collectParameterTokens(tokens: VbToken[], index: number): VbToken[] {
  const openIndex = nextNonBoundaryIndex(tokens, index);
  if (tokens[openIndex]?.text !== "(") {
    return [];
  }
  const parameters: VbToken[] = [];
  let canReadName = true;
  for (
    let cursor = openIndex + 1;
    cursor < tokens.length && tokens[cursor].text !== ")";
    cursor += 1
  ) {
    const token = tokens[cursor];
    const tokenLower = lower(token);
    if (token.text === ",") {
      canReadName = true;
      continue;
    }
    if (
      tokenLower === "optional" ||
      tokenLower === "byval" ||
      tokenLower === "byref" ||
      tokenLower === "paramarray"
    ) {
      continue;
    }
    if (canReadName && token.kind === "identifier") {
      parameters.push(token);
      canReadName = false;
    }
  }
  return parameters;
}

function declarationNameEntries(tokens: VbToken[], startIndex: number): DeclarationNameEntry[] {
  const names: DeclarationNameEntry[] = [];
  const endIndex = statementEndIndex(tokens, startIndex - 1);
  let canReadIdentifier = true;
  let depth = 0;
  for (let index = startIndex; index <= endIndex; index += 1) {
    const current = tokens[index];
    if (!current) {
      continue;
    }
    const currentLower = lower(current);
    if (current.text === "(") {
      depth += 1;
      canReadIdentifier = false;
      continue;
    }
    if (current.text === ")") {
      depth = Math.max(0, depth - 1);
      continue;
    }
    if (current.text === "," && depth === 0) {
      canReadIdentifier = true;
      continue;
    }
    if (current.text === "=" && depth === 0) {
      break;
    }
    if (currentLower === "as") {
      canReadIdentifier = false;
      continue;
    }
    if (current.kind === "identifier" && canReadIdentifier) {
      names.push({
        token: current,
        ...arrayMetadataAfterName(tokens, index, endIndex),
      });
      canReadIdentifier = false;
    }
  }
  return names;
}

function arrayMetadataAfterName(
  tokens: VbToken[],
  nameIndex: number,
  endIndex: number,
): Pick<DeclarationNameEntry, "arrayKind" | "arrayDimensions"> {
  const openIndex = nextNonBoundaryIndex(tokens, nameIndex + 1);
  if (openIndex === -1 || openIndex > endIndex || tokens[openIndex]?.text !== "(") {
    return {};
  }
  const closeIndex = matchingCloseParenIndex(tokens, openIndex, endIndex);
  if (closeIndex === -1) {
    return {};
  }
  const dimensions = arrayDimensionTexts(tokens, openIndex + 1, closeIndex);
  return {
    arrayKind: dimensions.length === 0 ? "dynamic" : "fixed",
    arrayDimensions: dimensions,
  };
}

function matchingCloseParenIndex(tokens: VbToken[], openIndex: number, endIndex: number): number {
  let depth = 0;
  for (let index = openIndex; index <= endIndex; index += 1) {
    const token = tokens[index];
    if (token?.text === "(") {
      depth += 1;
    } else if (token?.text === ")") {
      depth -= 1;
      if (depth === 0) {
        return index;
      }
    }
  }
  return -1;
}

function arrayDimensionTexts(tokens: VbToken[], startIndex: number, endIndex: number): string[] {
  const dimensions: string[] = [];
  let current = "";
  let depth = 0;
  for (let index = startIndex; index < endIndex; index += 1) {
    const token = tokens[index];
    if (!token || isBoundaryToken(token)) {
      continue;
    }
    if (token.text === "," && depth === 0) {
      dimensions.push(current.trim());
      current = "";
      continue;
    }
    if (token.text === "(") {
      depth += 1;
    } else if (token.text === ")") {
      depth = Math.max(0, depth - 1);
    }
    current += token.text;
  }
  const trailing = current.trim();
  if (trailing.length > 0 || dimensions.length > 0) {
    dimensions.push(trailing);
  }
  return dimensions.filter((dimension) => dimension.length > 0);
}

function resolveDeclaration(
  state: SymbolIndexBuildState,
  lookup: DeclarationLookup,
  token: VbToken,
  role: VbIndexedReferenceRole,
  baseName?: string,
): VbIndexedDeclaration | undefined {
  const normalized = normalizeName(token.text);
  if (role === "member") {
    if (baseName?.toLowerCase() === "me") {
      const classScope = activeScopeAt(state, token.start, "class");
      return findMatchingDeclaration(
        lookup.byParentName.get(declarationMapKey(classScope?.declarationId ?? "", normalized)),
        role,
      );
    }
    return undefined;
  }
  const procedureScope = activeScopeAt(state, token.start, "procedure");
  if (procedureScope) {
    const local = findMatchingDeclaration(
      lookup.byScopeName.get(declarationMapKey(procedureScope.id, normalized)),
      role,
    );
    if (local) {
      return local;
    }
  }
  const classScope = activeScopeAt(state, token.start, "class");
  if (classScope) {
    const member = findMatchingDeclaration(
      lookup.byParentName.get(declarationMapKey(classScope.declarationId ?? "", normalized)),
      role,
    );
    if (member) {
      return member;
    }
  }
  return findMatchingDeclaration(
    lookup.byScopeName.get(declarationMapKey(globalScopeId, normalized)),
    role,
  );
}

function findMatchingDeclaration(
  declarations: VbIndexedDeclaration[] | undefined,
  role: VbIndexedReferenceRole,
): VbIndexedDeclaration | undefined {
  return declarations?.find((declaration) => declarationKindMatchesRole(declaration.kind, role));
}

function declarationKindMatchesRole(
  kind: VbIndexedDeclarationKind,
  role: VbIndexedReferenceRole,
): boolean {
  if (role === "new") {
    return kind === "class";
  }
  if (role === "call") {
    return kind === "function" || kind === "sub" || kind === "method" || kind === "property";
  }
  return true;
}

function declarationCanBeIndexed(kind: VbIndexedDeclarationKind): boolean {
  return (
    kind === "variable" ||
    kind === "constant" ||
    kind === "parameter" ||
    kind === "field" ||
    kind === "property"
  );
}

function referenceRole(tokens: VbToken[], index: number): VbIndexedReferenceRole {
  const previous = previousInStatement(tokens, index);
  const next = nextInStatement(tokens, index + 1);
  if (previous?.text === ".") {
    return "member";
  }
  const previousLower = lower(previous);
  if (previousLower === "new") {
    return "new";
  }
  if (isWriteTarget(tokens, index)) {
    return "write";
  }
  if (previousLower === "call" || next?.text === "(" || isBareCallTarget(tokens, index)) {
    return "call";
  }
  return "read";
}

function isWriteTarget(tokens: VbToken[], index: number): boolean {
  const previous = previousInStatement(tokens, index);
  const next = nextInStatement(tokens, index + 1);
  if (next?.text !== "=") {
    return false;
  }
  const previousLower = lower(previous);
  return (
    isStatementFirstIdentifier(tokens, index) ||
    previousLower === "set" ||
    previousLower === "let" ||
    previousLower === "then" ||
    previousLower === "else"
  );
}

function isFunctionReturnAssignmentReference(
  state: SymbolIndexBuildState,
  index: number,
  resolved: VbIndexedDeclaration,
): boolean {
  const token = state.tokens[index];
  const scope = activeScopeAt(state, token.start, "procedure");
  if (
    scope?.procedureKind !== "function" ||
    !scope.declarationId ||
    scope.declarationId !== resolved.id ||
    scope.normalizedName !== normalizeName(token.text)
  ) {
    return false;
  }
  const targetIndex = assignmentTargetIndex(state.tokens, index);
  return targetIndex === index && statementHasEqualsAfter(state.tokens, index);
}

function assignmentTargetIndex(tokens: VbToken[], index: number): number {
  let cursor = index;
  while (cursor > 0 && !isStatementBoundary(tokens[cursor - 1])) {
    cursor -= 1;
  }
  const first = lower(tokens[cursor]);
  return first === "set" || first === "let" ? nextNonBoundaryIndex(tokens, cursor + 1) : cursor;
}

function statementHasEqualsAfter(tokens: VbToken[], index: number): boolean {
  for (
    let cursor = index + 1;
    cursor < tokens.length && !isStatementBoundary(tokens[cursor]);
    cursor += 1
  ) {
    if (tokens[cursor].text === "=") {
      return true;
    }
  }
  return false;
}

function isBareCallTarget(tokens: VbToken[], index: number): boolean {
  if (!isStatementFirstIdentifier(tokens, index)) {
    return false;
  }
  const next = nextInStatement(tokens, index + 1);
  return Boolean(next && next.kind !== "newline" && !["=", ".", ")", ","].includes(next.text));
}

function isStatementFirstIdentifier(tokens: VbToken[], index: number): boolean {
  let cursor = index - 1;
  while (cursor >= 0 && !isStatementBoundary(tokens[cursor])) {
    if (!isBoundaryToken(tokens[cursor])) {
      return false;
    }
    cursor -= 1;
  }
  return true;
}

function expectedKindsForReference(
  role: VbIndexedReferenceRole,
  resolved: VbIndexedDeclaration | undefined,
): VbIndexedDeclarationKind[] | undefined {
  if (resolved) {
    return undefined;
  }
  if (role === "new") {
    return classExpectedKinds;
  }
  if (role === "call") {
    return callableExpectedKinds;
  }
  if (role === "member") {
    return memberExpectedKinds;
  }
  if (role === "write") {
    return writableExpectedKinds;
  }
  if (role === "read") {
    return valueExpectedKinds;
  }
  return undefined;
}

function bindingScopeForReference(
  resolved: VbIndexedDeclaration | undefined,
  expectedKinds: VbIndexedDeclarationKind[] | undefined,
): VbBindingScope | undefined {
  if (resolved?.bindingScope) {
    return resolved.bindingScope;
  }
  return expectedKinds?.some((kind) => kind === "variable" || kind === "constant")
    ? "unknown"
    : undefined;
}

function activeScopeAt(
  state: SymbolIndexBuildState,
  offset: number,
  kind: "class" | "procedure",
): ScopeFrame | undefined {
  for (let index = state.scopes.length - 1; index >= 0; index -= 1) {
    const scope = state.scopes[index];
    if (scope.kind === kind && scope.start <= offset && offset <= scope.end) {
      return scope;
    }
  }
  return undefined;
}

function currentScope(state: SymbolIndexBuildState): ScopeFrame {
  return state.stack.at(-1) ?? state.stack[0];
}

function previousInStatement(tokens: VbToken[], index: number): VbToken | undefined {
  for (let cursor = index - 1; cursor >= 0; cursor -= 1) {
    if (isStatementBoundary(tokens[cursor])) {
      break;
    }
    if (!isBoundaryToken(tokens[cursor])) {
      return tokens[cursor];
    }
  }
  return undefined;
}

function nextInStatement(tokens: VbToken[], index: number): VbToken | undefined {
  for (let cursor = index; cursor < tokens.length; cursor += 1) {
    if (isStatementBoundary(tokens[cursor])) {
      break;
    }
    if (!isBoundaryToken(tokens[cursor])) {
      return tokens[cursor];
    }
  }
  return undefined;
}

function nextNonBoundaryIndex(tokens: VbToken[], index: number): number {
  for (let cursor = index; cursor < tokens.length; cursor += 1) {
    if (!isBoundaryToken(tokens[cursor])) {
      return cursor;
    }
  }
  return -1;
}

function nextTokenIndexInStatement(
  tokens: VbToken[],
  index: number,
  endIndex: number,
  text?: string,
): number {
  for (let cursor = index; cursor <= endIndex; cursor += 1) {
    const token = tokens[cursor];
    if (!token || isStatementBoundary(token)) {
      break;
    }
    if (!isBoundaryToken(token) && (text === undefined || token.text === text)) {
      return cursor;
    }
  }
  return -1;
}

function memberBaseName(tokens: VbToken[], index: number): string | undefined {
  if (previousInStatement(tokens, index)?.text !== ".") {
    return undefined;
  }
  const dotIndex = previousInStatementIndex(tokens, index);
  const base = dotIndex === -1 ? undefined : previousInStatement(tokens, dotIndex);
  return base?.kind === "identifier" ? base.text : undefined;
}

function previousInStatementIndex(tokens: VbToken[], index: number): number {
  for (let cursor = index - 1; cursor >= 0; cursor -= 1) {
    if (isStatementBoundary(tokens[cursor])) {
      break;
    }
    if (!isBoundaryToken(tokens[cursor])) {
      return cursor;
    }
  }
  return -1;
}

function isTypeAnnotationIdentifier(tokens: VbToken[], index: number): boolean {
  return lower(previousInStatement(tokens, index)) === "as";
}

function isStatementStart(tokens: VbToken[], index: number): boolean {
  const previous = tokens[index - 1];
  return !previous || isStatementBoundary(previous);
}

function statementEndIndex(tokens: VbToken[], startIndex: number): number {
  let index = startIndex;
  while (index + 1 < tokens.length) {
    const next = tokens[index + 1];
    if ((next.kind === "newline" && tokens[index]?.text !== "_") || next.text === ":") {
      break;
    }
    index += 1;
  }
  return index;
}

function isStatementBoundary(token: VbToken | undefined): boolean {
  return !token || token.kind === "newline" || token.text === ":";
}

function isBoundaryToken(token: VbToken | undefined): boolean {
  return !token || token.kind === "newline";
}

function lower(token: VbToken | undefined): string | undefined {
  return token?.text.toLowerCase();
}

function normalizeName(name: string): string {
  return name.toLowerCase();
}

function rangeAt(state: SymbolIndexBuildState, start: number, end: number): Range {
  return {
    start: positionAt(state.lineStarts, state.text.length, start),
    end: positionAt(state.lineStarts, state.text.length, end),
  };
}

function positionAt(lineStarts: number[], textLength: number, offset: number): Range["start"] {
  const safeOffset = Math.max(0, Math.min(offset, textLength));
  let low = 0;
  let high = lineStarts.length - 1;
  let line = 0;
  while (low <= high) {
    const middle = Math.floor((low + high) / 2);
    if (lineStarts[middle] <= safeOffset) {
      line = middle;
      low = middle + 1;
    } else {
      high = middle - 1;
    }
  }
  return { line, character: safeOffset - lineStarts[line] };
}

function lineStartsForText(text: string): number[] {
  const starts = [0];
  for (let index = 0; index < text.length; index += 1) {
    if (text.charCodeAt(index) === 10) {
      starts.push(index + 1);
    }
  }
  return starts;
}

function tokenKey(token: VbToken): string {
  return `${token.start}:${token.end}`;
}

function declarationId(uri: string, kind: VbIndexedDeclarationKind, token: VbToken): string {
  return `${uri}#${kind}:${normalizeName(token.text)}@${token.start}`;
}

function declarationMapKey(scopeId: string, normalizedName: string): string {
  return `${scopeId}\0${normalizedName}`;
}

function referenceKey(uri: string, token: VbToken): string {
  return `${uri}#ref:${normalizeName(token.text)}@${token.start}`;
}

function findLastIndex<T>(items: T[], predicate: (item: T) => boolean): number {
  for (let index = items.length - 1; index >= 0; index -= 1) {
    if (predicate(items[index])) {
      return index;
    }
  }
  return -1;
}
