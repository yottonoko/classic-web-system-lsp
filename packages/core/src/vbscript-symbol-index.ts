import type { Range } from "vscode-languageserver-types";
import type { AspInclude, AspRegion, AspSettings, VbToken } from "./types";
import { extractAspIncludeRefs, normalizeScriptLanguage, parseAttributes } from "./asp-scanner";
import { tokenizeVbscript } from "./vbscript-cst";

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

type VbIndexedProcedureKind = "sub" | "function" | "property";

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
  scopeId?: string;
  parentId?: string;
  memberOf?: string;
  visibility?: "public" | "private";
  bindingScope?: VbBindingScope;
  parameters?: VbIndexedParameter[];
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
  lineStarts: number[];
  scopes: ScopeFrame[];
  stack: ScopeFrame[];
  tokens: VbToken[];
}

interface VbRegionIndexInput {
  regions: AspRegion[];
  includeRefs: AspInclude[];
}

interface DeclarationLookup {
  byId: Map<string, VbIndexedDeclaration>;
  byScopeName: Map<string, VbIndexedDeclaration[]>;
  byParentName: Map<string, VbIndexedDeclaration[]>;
}

interface IndexHtmlTag {
  name: "script" | "style";
  start: number;
  end: number;
  attributes: Record<string, string | true>;
  closing: boolean;
  selfClosing: boolean;
}

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
  collectDeclarations(state);
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
  const candidates: AspRegion[] = [];
  let directiveLanguage: string | undefined;
  const specialPattern = /<!--|<%|<\/?script\b|<\/?style\b/gi;
  let match: RegExpExecArray | null;
  while ((match = specialPattern.exec(text)) !== null) {
    const start = match.index;
    if (isInsideHtmlTagAt(text, start)) {
      continue;
    }
    const token = match[0].toLowerCase();
    if (token === "<!--") {
      const commentEnd = text.indexOf("-->", start + 4);
      specialPattern.lastIndex = commentEnd === -1 ? text.length : commentEnd + 3;
      continue;
    }
    if (token === "<%") {
      const close = text.indexOf("%>", start + 2);
      const marker = text[start + 2];
      const kind =
        marker === "=" ? "asp-expression" : marker === "@" ? "asp-directive" : "asp-block";
      const contentStart = start + (marker === "=" || marker === "@" ? 3 : 2);
      const contentEnd = close === -1 ? text.length : close;
      const region: AspRegion = {
        kind,
        language: kind === "asp-directive" ? "asp-directive" : "vbscript",
        start,
        end: close === -1 ? text.length : close + 2,
        contentStart,
        contentEnd,
      };
      candidates.push(region);
      if (kind === "asp-directive" && !directiveLanguage) {
        directiveLanguage = directiveLanguageFromRegion(text, region);
      }
      specialPattern.lastIndex = region.end;
      continue;
    }
    const tag = readIndexHtmlTag(text, start);
    if (!tag) {
      continue;
    }
    if ((tag.name === "script" || tag.name === "style") && !tag.closing && !tag.selfClosing) {
      const close = findElementClose(text, tag.name, tag.end);
      if (tag.name === "script" && isServerScriptTag(tag)) {
        candidates.push({
          kind: "server-script",
          language:
            normalizeScriptLanguage(
              String(tag.attributes.language ?? tag.attributes.type ?? "VBScript"),
            ).toLowerCase() === "jscript"
              ? "jscript"
              : "vbscript",
          start: tag.start,
          end: close?.end ?? text.length,
          contentStart: tag.end,
          contentEnd: close?.start ?? text.length,
          attributes: tag.attributes,
        });
      }
      specialPattern.lastIndex = close?.end ?? tag.end;
    }
  }
  const defaultLanguage = normalizeScriptLanguage(
    directiveLanguage ?? settings.defaultLanguage ?? "VBScript",
  );
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
  return { regions, includeRefs: extractAspIncludeRefs(text) };
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

function isInsideHtmlTagAt(text: string, index: number): boolean {
  const tagStart = text.lastIndexOf("<", index - 1);
  if (tagStart === -1 || text.startsWith("<!--", tagStart) || text.startsWith("<%", tagStart)) {
    return false;
  }
  const tagEnd = text.lastIndexOf(">", index - 1);
  return tagStart > tagEnd;
}

function readIndexHtmlTag(text: string, start: number): IndexHtmlTag | undefined {
  if (text[start] !== "<") {
    return undefined;
  }
  let cursor = start + 1;
  const closing = text[cursor] === "/";
  if (closing) {
    cursor += 1;
  }
  while (isHtmlWhitespaceCode(text.charCodeAt(cursor))) {
    cursor += 1;
  }
  const nameStart = cursor;
  while (isAsciiAlphaCode(text.charCodeAt(cursor))) {
    cursor += 1;
  }
  const name = text.slice(nameStart, cursor).toLowerCase();
  if (name !== "script" && name !== "style") {
    return undefined;
  }
  const tagEnd = findTagEnd(text, cursor);
  if (tagEnd === -1) {
    return undefined;
  }
  const attributesText = text.slice(cursor, tagEnd).replace(/\/\s*$/, "");
  return {
    name,
    start,
    end: tagEnd + 1,
    attributes: parseAttributes(attributesText),
    closing,
    selfClosing: /\/\s*$/.test(text.slice(cursor, tagEnd)),
  };
}

function isServerScriptTag(tag: IndexHtmlTag): boolean {
  return String(tag.attributes.runat ?? "").toLowerCase() === "server";
}

function findElementClose(
  text: string,
  tagName: "script" | "style",
  offset: number,
): { start: number; end: number } | undefined {
  const pattern = new RegExp(`</\\s*${tagName}\\b`, "gi");
  pattern.lastIndex = offset;
  const match = pattern.exec(text);
  if (!match) {
    return undefined;
  }
  const tagEnd = findTagEnd(text, match.index + match[0].length);
  if (tagEnd === -1) {
    return undefined;
  }
  return { start: match.index, end: tagEnd + 1 };
}

function findTagEnd(text: string, offset: number): number {
  let quote: string | undefined;
  for (let index = offset; index < text.length; index += 1) {
    const char = text[index];
    if (quote) {
      if (char === quote) {
        quote = undefined;
      }
      continue;
    }
    if (char === '"' || char === "'") {
      quote = char;
      continue;
    }
    if (char === ">") {
      return index;
    }
  }
  return -1;
}

function isAsciiAlphaCode(code: number): boolean {
  return (code >= 65 && code <= 90) || (code >= 97 && code <= 122);
}

function isHtmlWhitespaceCode(code: number): boolean {
  return code === 32 || code === 9 || code === 10 || code === 12 || code === 13;
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

function collectDeclarations(state: SymbolIndexBuildState): void {
  const tokens = state.tokens;
  for (let index = 0; index < tokens.length; index += 1) {
    const token = tokens[index];
    if (!isStatementStart(tokens, index) || isBoundaryToken(token)) {
      continue;
    }
    const first = lower(token);
    const second = lower(tokens[index + 1]);
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
      addVariableDeclarations(state, "variable", token, index + 1, undefined);
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
): void {
  const current = currentScope(state);
  const classScope = current.kind === "class" ? current : undefined;
  const procedureScope = current.kind === "procedure" ? current : undefined;
  const kind: VbIndexedDeclarationKind = baseKind === "variable" && classScope ? "field" : baseKind;
  const bindingScope: VbBindingScope | undefined =
    kind === "field" || classScope ? undefined : procedureScope ? "local" : "global";
  for (const nameToken of declarationNameTokens(state.tokens, startIndex)) {
    addDeclaration(state, {
      kind,
      nameToken,
      start: startToken.start,
      end: nameToken.end,
      scopeId: classScope?.id ?? procedureScope?.id ?? globalScopeId,
      parentId: classScope?.declarationId,
      memberOf: classScope?.name,
      visibility,
      bindingScope,
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
    parameters?: VbIndexedParameter[];
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
    parameters: input.parameters,
  };
  state.declarations.push(declaration);
  state.declarationNameKeys.add(tokenKey(input.nameToken));
  return declaration;
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
    if (!resolved && expectedKinds) {
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
  return { references, callSites, deferredExternalRefs };
}

function declarationLookup(declarations: VbIndexedDeclaration[]): DeclarationLookup {
  const lookup: DeclarationLookup = {
    byId: new Map(),
    byScopeName: new Map(),
    byParentName: new Map(),
  };
  for (const declaration of declarations) {
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
  return lookup;
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

function declarationNameTokens(tokens: VbToken[], startIndex: number): VbToken[] {
  const names: VbToken[] = [];
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
      names.push(current);
      canReadIdentifier = false;
    }
  }
  return names;
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
  return (
    isStatementFirstIdentifier(tokens, index) ||
    lower(previous) === "set" ||
    lower(previous) === "let"
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
