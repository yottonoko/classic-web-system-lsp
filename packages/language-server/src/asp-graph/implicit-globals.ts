import type { Position, Range } from "vscode-languageserver/node";
import type { AspInclude, AspSettings, VbSymbolIndex } from "@asp-lsp/core";
import type {
  AnalysisCancellation,
  AspGraphIndexedDocument,
  GraphFileIndex,
  PrecomputedIncludeReachability,
} from "./types";

export interface ImplicitGlobalDocumentMetadata {
  fileKey: string;
  uri: string;
  declarations: VbSymbolIndex["declarations"];
  includeRefs: AspInclude[];
}

export interface ImplicitGlobalIncludeGraph {
  directIncludesByOwnerKey: Map<string, Array<{ range: Range; targetKey: string }>>;
  parentIncludesByTargetKey: Map<string, Array<{ ownerKey: string; range: Range }>>;
}

export interface ImplicitGlobalCanonicalIdResult {
  canonicalIdById: Map<string, string>;
  groups: number;
  maxGroupSize: number;
}

interface IncludeVisibilityMemo {
  cache: Map<string, boolean>;
  visiting: Set<string>;
}

export interface BuildImplicitGlobalIncludeGraphHost {
  graphFileKey(fileName: string): string;
  normalizeFileName(fileName: string): string;
  resolveIncludePathDetailsAsync(
    ownerUri: string,
    includePath: string,
    mode: AspInclude["mode"],
    settings: AspSettings,
  ): Promise<{ fileName: string }>;
}

export function implicitGlobalDocumentMetadataFromIndexed(
  indexed: AspGraphIndexedDocument,
  fileKey: string,
): ImplicitGlobalDocumentMetadata {
  return {
    fileKey,
    uri: indexed.document.uri,
    declarations: indexed.graphIndex.vbSymbolIndex.declarations,
    includeRefs: indexed.graphIndex.includeRefs,
  };
}

export async function buildImplicitGlobalIncludeGraphAsync(
  metadata: readonly ImplicitGlobalDocumentMetadata[],
  settings: AspSettings,
  host: BuildImplicitGlobalIncludeGraphHost,
  cancellation: AnalysisCancellation,
): Promise<ImplicitGlobalIncludeGraph> {
  const indexedFileKeys = new Set(metadata.map((item) => item.fileKey));
  const includeGraph: ImplicitGlobalIncludeGraph = {
    directIncludesByOwnerKey: new Map(),
    parentIncludesByTargetKey: new Map(),
  };
  for (const item of metadata) {
    throwIfCancelled(cancellation);
    for (const include of item.includeRefs) {
      const resolved = await host.resolveIncludePathDetailsAsync(
        item.uri,
        include.path,
        include.mode,
        settings,
      );
      const targetKey = host.graphFileKey(host.normalizeFileName(resolved.fileName));
      if (!indexedFileKeys.has(targetKey)) {
        continue;
      }
      pushMapItem(includeGraph.directIncludesByOwnerKey, item.fileKey, {
        range: include.range,
        targetKey,
      });
      pushMapItem(includeGraph.parentIncludesByTargetKey, targetKey, {
        ownerKey: item.fileKey,
        range: include.range,
      });
    }
  }
  return includeGraph;
}

export function computeImplicitGlobalCanonicalIds(
  metadata: readonly ImplicitGlobalDocumentMetadata[],
  includeGraph: ImplicitGlobalIncludeGraph,
  cancellation: AnalysisCancellation,
): ImplicitGlobalCanonicalIdResult {
  if (metadata.length < 2) {
    return { canonicalIdById: new Map(), groups: 0, maxGroupSize: 0 };
  }
  const declarationFileKeyById = new Map<string, string>();
  const declarationOrderById = new Map<string, number>();
  const declarationsByName = new Map<string, Array<VbSymbolIndex["declarations"][number]>>();
  let declarationOrder = 0;
  for (const item of metadata) {
    for (const declaration of item.declarations) {
      declarationFileKeyById.set(declaration.id, item.fileKey);
      declarationOrderById.set(declaration.id, declarationOrder);
      declarationOrder += 1;
      if (isImplicitGlobalMergeDeclaration(declaration)) {
        pushMapItem(declarationsByName, declaration.normalizedName, declaration);
      }
    }
  }
  const summary = implicitGlobalGroupSummary(declarationsByName.values());
  if (declarationsByName.size === 0) {
    return { canonicalIdById: new Map(), ...summary };
  }
  const targetKeys = new Set<string>();
  for (const declarations of declarationsByName.values()) {
    for (const declaration of declarations) {
      const fileKey = declarationFileKeyById.get(declaration.id);
      if (fileKey) {
        targetKeys.add(fileKey);
      }
    }
  }
  const reachability = precomputeIncludeReachability(includeGraph, targetKeys);
  const visibilityMemo = createIncludeVisibilityMemo();
  const union = new ImplicitGlobalUnionFind();
  for (const declarations of declarationsByName.values()) {
    throwIfCancelled(cancellation);
    if (
      declarations.length < 2 ||
      !declarations.some((declaration) => declaration.implicitGlobal === true)
    ) {
      continue;
    }
    if (reachability.hasCycle) {
      unionImplicitGlobalDeclarationsPairwise(
        declarations,
        declarationFileKeyById,
        includeGraph,
        reachability,
        visibilityMemo,
        union,
      );
    } else {
      unionImplicitGlobalDeclarationsByReachability(
        declarations,
        declarationFileKeyById,
        includeGraph,
        reachability,
        visibilityMemo,
        union,
      );
    }
  }
  const declarationsByRoot = new Map<string, Array<VbSymbolIndex["declarations"][number]>>();
  for (const declarations of declarationsByName.values()) {
    for (const declaration of declarations) {
      const root = union.find(declaration.id);
      if (root !== declaration.id || union.size(root) > 1) {
        pushMapItem(declarationsByRoot, root, declaration);
      }
    }
  }
  const canonicalIdById = new Map<string, string>();
  for (const declarations of declarationsByRoot.values()) {
    const canonical = implicitGlobalCanonicalDeclaration(
      declarations,
      declarationOrderById,
      declarationFileKeyById,
      includeGraph,
      reachability,
      visibilityMemo,
    );
    for (const declaration of declarations) {
      canonicalIdById.set(declaration.id, canonical.id);
    }
  }
  return { canonicalIdById, ...summary };
}

export function canonicalizeImplicitGlobalIndexedDocument(
  indexed: AspGraphIndexedDocument,
  canonicalIdById: Map<string, string>,
  graphFileIndexFingerprint: (index: VbSymbolIndex) => string,
): AspGraphIndexedDocument {
  if (![...canonicalIdById].some(([id, canonicalId]) => id !== canonicalId)) {
    return indexed;
  }
  const index = indexed.graphIndex.vbSymbolIndex;
  const canonicalDeclarationIds = new Set(canonicalIdById.values());
  const declarations = index.declarations.filter((declaration) => {
    const canonicalId = canonicalIdById.get(declaration.id);
    return (
      !canonicalId || canonicalId === declaration.id || canonicalDeclarationIds.has(declaration.id)
    );
  });
  const canonicalResolvedId = (resolvedId: string | undefined): string | undefined =>
    resolvedId ? (canonicalIdById.get(resolvedId) ?? resolvedId) : undefined;
  const references = index.references.map((reference) => ({
    ...reference,
    resolvedId: canonicalResolvedId(reference.resolvedId),
  }));
  const callSites = index.callSites.map((callSite) => ({
    ...callSite,
    resolvedId: canonicalResolvedId(callSite.resolvedId),
  }));
  const deferredExternalRefs = index.deferredExternalRefs.map((ref) => ({
    ...ref,
    localResolutionId: canonicalResolvedId(ref.localResolutionId),
  }));
  const vbSymbolIndex: VbSymbolIndex = {
    ...index,
    declarations,
    references,
    callSites,
    deferredExternalRefs,
    stats: {
      ...index.stats,
      declarations: declarations.length,
      references: references.length,
      callSites: callSites.length,
      deferredExternalRefs: deferredExternalRefs.length,
    },
  };
  return {
    ...indexed,
    graphIndex: {
      ...indexed.graphIndex,
      vbSymbolIndex,
      fingerprint: graphFileIndexFingerprint(vbSymbolIndex),
    },
  };
}

export function serializableGraphFileIndex(index: GraphFileIndex): Omit<
  GraphFileIndex,
  "typeHints"
> & {
  typeHints: Array<[string, GraphFileIndex["typeHints"] extends Map<string, infer V> ? V : never]>;
} {
  return {
    ...index,
    typeHints: [...index.typeHints.entries()],
  };
}

export function hydrateGraphFileIndex(
  index: ReturnType<typeof serializableGraphFileIndex>,
): GraphFileIndex {
  return {
    ...index,
    typeHints: new Map(index.typeHints),
  };
}

class ImplicitGlobalUnionFind {
  private readonly parents = new Map<string, string>();
  private readonly sizes = new Map<string, number>();

  find(id: string): string {
    const parent = this.parents.get(id);
    if (!parent) {
      this.parents.set(id, id);
      this.sizes.set(id, 1);
      return id;
    }
    if (parent === id) {
      return id;
    }
    const root = this.find(parent);
    this.parents.set(id, root);
    return root;
  }

  union(left: string, right: string): void {
    const leftRoot = this.find(left);
    const rightRoot = this.find(right);
    if (leftRoot === rightRoot) {
      return;
    }
    const leftSize = this.sizes.get(leftRoot) ?? 1;
    const rightSize = this.sizes.get(rightRoot) ?? 1;
    const [parent, child, size] =
      leftSize >= rightSize
        ? [leftRoot, rightRoot, leftSize + rightSize]
        : [rightRoot, leftRoot, leftSize + rightSize];
    this.parents.set(child, parent);
    this.sizes.set(parent, size);
    this.sizes.delete(child);
  }

  size(id: string): number {
    return this.sizes.get(this.find(id)) ?? 1;
  }
}

interface IncludeReachabilityGraph {
  directIncludesByOwnerKey: Map<string, Array<{ targetKey: string }>>;
  parentIncludesByTargetKey: Map<string, Array<{ ownerKey: string }>>;
}

function isImplicitGlobalMergeDeclaration(
  declaration: VbSymbolIndex["declarations"][number],
): boolean {
  return (
    declaration.kind === "variable" &&
    declaration.bindingScope === "global" &&
    !declaration.memberOf &&
    (declaration.implicitGlobal === true || declaration.implicit !== true)
  );
}

function implicitGlobalCanonicalDeclaration(
  declarations: Array<VbSymbolIndex["declarations"][number]>,
  declarationOrderById: Map<string, number>,
  declarationFileKeyById: Map<string, string>,
  includeGraph: ImplicitGlobalIncludeGraph,
  reachability: PrecomputedIncludeReachability,
  visibilityMemo: IncludeVisibilityMemo,
): VbSymbolIndex["declarations"][number] {
  const visibilityScoreById = new Map(
    declarations.map((declaration) => [
      declaration.id,
      implicitGlobalCanonicalVisibilityScore(
        declaration,
        declarations,
        declarationFileKeyById,
        includeGraph,
        reachability,
        visibilityMemo,
      ),
    ]),
  );
  return [...declarations].sort(
    (left, right) =>
      (visibilityScoreById.get(left.id) ?? 1) - (visibilityScoreById.get(right.id) ?? 1) ||
      implicitGlobalCanonicalScore(left) - implicitGlobalCanonicalScore(right) ||
      (declarationOrderById.get(left.id) ?? 0) - (declarationOrderById.get(right.id) ?? 0),
  )[0];
}

function implicitGlobalCanonicalVisibilityScore(
  declaration: VbSymbolIndex["declarations"][number],
  declarations: Array<VbSymbolIndex["declarations"][number]>,
  declarationFileKeyById: Map<string, string>,
  includeGraph: ImplicitGlobalIncludeGraph,
  reachability: PrecomputedIncludeReachability,
  visibilityMemo: IncludeVisibilityMemo,
): number {
  const targetKey = declarationFileKeyById.get(declaration.id);
  if (!targetKey) {
    return 1;
  }
  return declarations.every((candidate) => {
    if (candidate.id === declaration.id) {
      return true;
    }
    const ownerKey = declarationFileKeyById.get(candidate.id);
    return (
      ownerKey !== undefined &&
      isImplicitGlobalDeclarationVisibleFromFile(
        includeGraph,
        ownerKey,
        declaration,
        targetKey,
        candidate.nameRange,
        reachability,
        visibilityMemo,
      )
    );
  })
    ? 0
    : 1;
}

function implicitGlobalCanonicalScore(declaration: VbSymbolIndex["declarations"][number]): number {
  if (declaration.implicit !== true) {
    return 0;
  }
  return declaration.implicitGlobalCandidate === true ? 2 : 1;
}

function unionImplicitGlobalDeclarationsPairwise(
  declarations: Array<VbSymbolIndex["declarations"][number]>,
  declarationFileKeyById: Map<string, string>,
  includeGraph: ImplicitGlobalIncludeGraph,
  reachability: PrecomputedIncludeReachability,
  visibilityMemo: IncludeVisibilityMemo,
  union: ImplicitGlobalUnionFind,
): void {
  for (let leftIndex = 0; leftIndex < declarations.length; leftIndex += 1) {
    const left = declarations[leftIndex];
    const leftFileKey = declarationFileKeyById.get(left.id);
    if (!leftFileKey) {
      continue;
    }
    for (let rightIndex = leftIndex + 1; rightIndex < declarations.length; rightIndex += 1) {
      const right = declarations[rightIndex];
      const rightFileKey = declarationFileKeyById.get(right.id);
      if (!rightFileKey || union.find(left.id) === union.find(right.id)) {
        continue;
      }
      if (
        implicitGlobalDeclarationsCanMerge(
          left,
          leftFileKey,
          right,
          rightFileKey,
          includeGraph,
          reachability,
          visibilityMemo,
        )
      ) {
        union.union(left.id, right.id);
      }
    }
  }
}

function unionImplicitGlobalDeclarationsByReachability(
  declarations: Array<VbSymbolIndex["declarations"][number]>,
  declarationFileKeyById: Map<string, string>,
  includeGraph: ImplicitGlobalIncludeGraph,
  reachability: PrecomputedIncludeReachability,
  visibilityMemo: IncludeVisibilityMemo,
  union: ImplicitGlobalUnionFind,
): void {
  const processedByFileKey = new Map<string, Array<VbSymbolIndex["declarations"][number]>>();
  const reachableTargetsByOwnerKey = new Map<string, Set<string>>();
  for (const declaration of declarations) {
    const declarationFileKey = declarationFileKeyById.get(declaration.id);
    if (!declarationFileKey) {
      continue;
    }
    const candidatesByRoot = reachableImplicitGlobalMergeCandidatesByRoot(
      declaration,
      declarationFileKey,
      processedByFileKey,
      declarationFileKeyById,
      includeGraph,
      reachability,
      reachableTargetsByOwnerKey,
      union,
    );
    for (const candidates of candidatesByRoot.values()) {
      if (candidates.some((candidate) => union.find(candidate.id) === union.find(declaration.id))) {
        continue;
      }
      const visibleCandidate = candidates.find((candidate) => {
        const candidateFileKey = declarationFileKeyById.get(candidate.id);
        return (
          candidateFileKey !== undefined &&
          union.find(candidate.id) !== union.find(declaration.id) &&
          implicitGlobalDeclarationsCanMerge(
            declaration,
            declarationFileKey,
            candidate,
            candidateFileKey,
            includeGraph,
            reachability,
            visibilityMemo,
          )
        );
      });
      if (visibleCandidate) {
        union.union(declaration.id, visibleCandidate.id);
      }
    }
    pushMapItem(processedByFileKey, declarationFileKey, declaration);
  }
}

function reachableImplicitGlobalMergeCandidatesByRoot(
  declaration: VbSymbolIndex["declarations"][number],
  declarationFileKey: string,
  processedByFileKey: Map<string, Array<VbSymbolIndex["declarations"][number]>>,
  declarationFileKeyById: Map<string, string>,
  includeGraph: ImplicitGlobalIncludeGraph,
  reachability: PrecomputedIncludeReachability,
  reachableTargetsByOwnerKey: Map<string, Set<string>>,
  union: ImplicitGlobalUnionFind,
): Map<string, Array<VbSymbolIndex["declarations"][number]>> {
  const candidatesByRoot = new Map<string, Array<VbSymbolIndex["declarations"][number]>>();
  const addCandidatesFromFileKey = (fileKey: string): void => {
    for (const candidate of processedByFileKey.get(fileKey) ?? []) {
      const candidateFileKey = declarationFileKeyById.get(candidate.id);
      if (!candidateFileKey) {
        continue;
      }
      const root = union.find(candidate.id);
      if (root === union.find(declaration.id)) {
        continue;
      }
      pushMapItem(candidatesByRoot, root, candidate);
    }
  };
  addCandidatesFromFileKey(declarationFileKey);
  for (const targetKey of reachableImplicitGlobalIncludeTargets(
    includeGraph,
    declarationFileKey,
    reachableTargetsByOwnerKey,
  )) {
    addCandidatesFromFileKey(targetKey);
  }
  for (const ownerKey of reachability.reachingFileKeysByTarget.get(declarationFileKey) ?? []) {
    addCandidatesFromFileKey(ownerKey);
  }
  return candidatesByRoot;
}

function reachableImplicitGlobalIncludeTargets(
  includeGraph: ImplicitGlobalIncludeGraph,
  ownerKey: string,
  cache: Map<string, Set<string>>,
): Set<string> {
  let cached = cache.get(ownerKey);
  if (cached) {
    return cached;
  }
  cached = new Set();
  const queue = [...(includeGraph.directIncludesByOwnerKey.get(ownerKey) ?? [])].map(
    (include) => include.targetKey,
  );
  for (let index = 0; index < queue.length; index += 1) {
    const targetKey = queue[index];
    if (cached.has(targetKey)) {
      continue;
    }
    cached.add(targetKey);
    for (const include of includeGraph.directIncludesByOwnerKey.get(targetKey) ?? []) {
      queue.push(include.targetKey);
    }
  }
  cache.set(ownerKey, cached);
  return cached;
}

function implicitGlobalDeclarationsCanMerge(
  left: VbSymbolIndex["declarations"][number],
  leftFileKey: string,
  right: VbSymbolIndex["declarations"][number],
  rightFileKey: string,
  includeGraph: ImplicitGlobalIncludeGraph,
  reachability: PrecomputedIncludeReachability,
  visibilityMemo: IncludeVisibilityMemo,
): boolean {
  return (
    isImplicitGlobalDeclarationVisibleFromFile(
      includeGraph,
      leftFileKey,
      right,
      rightFileKey,
      left.nameRange,
      reachability,
      visibilityMemo,
    ) ||
    isImplicitGlobalDeclarationVisibleFromFile(
      includeGraph,
      rightFileKey,
      left,
      leftFileKey,
      right.nameRange,
      reachability,
      visibilityMemo,
    )
  );
}

function createIncludeVisibilityMemo(): IncludeVisibilityMemo {
  return { cache: new Map(), visiting: new Set() };
}

function includeVisibilityMemoKey(ownerKey: string, targetKey: string, range: Range): string {
  return `${ownerKey}\0${targetKey}\0${range.start.line}:${range.start.character}`;
}

function memoizedIncludeVisibility(
  memo: IncludeVisibilityMemo,
  key: string,
  compute: () => boolean,
): boolean {
  const cached = memo.cache.get(key);
  if (cached !== undefined) {
    return cached;
  }
  if (memo.visiting.has(key)) {
    return false;
  }
  memo.visiting.add(key);
  try {
    const value = compute();
    memo.cache.set(key, value);
    return value;
  } finally {
    memo.visiting.delete(key);
  }
}

function isImplicitGlobalDeclarationVisibleFromFile(
  includeGraph: ImplicitGlobalIncludeGraph,
  ownerKey: string,
  declaration: VbSymbolIndex["declarations"][number],
  declarationKey: string,
  referenceRange: Range,
  reachability: PrecomputedIncludeReachability,
  visibilityMemo: IncludeVisibilityMemo,
): boolean {
  if (declarationKey === ownerKey) {
    return true;
  }
  return isImplicitGlobalDeclarationVisibleFromFileAt(
    includeGraph,
    ownerKey,
    declaration,
    declarationKey,
    referenceRange,
    reachability,
    visibilityMemo,
    new Set([ownerKey]),
  );
}

function isImplicitGlobalDeclarationVisibleFromFileAt(
  includeGraph: ImplicitGlobalIncludeGraph,
  ownerKey: string,
  declaration: VbSymbolIndex["declarations"][number],
  declarationKey: string,
  referenceRange: Range,
  reachability: PrecomputedIncludeReachability,
  visibilityMemo: IncludeVisibilityMemo,
  visited: Set<string>,
): boolean {
  const key = includeVisibilityMemoKey(
    ownerKey,
    `${declarationKey}\0${declaration.id}`,
    referenceRange,
  );
  return memoizedIncludeVisibility(visibilityMemo, key, () => {
    if (declarationKey === ownerKey) {
      return positionBeforeOrEqual(declaration.nameRange.start, referenceRange.start);
    }
    if (
      hasEarlierReachableImplicitGlobalInclude(
        includeGraph,
        ownerKey,
        declarationKey,
        referenceRange,
        reachability,
      )
    ) {
      return true;
    }
    for (const parentInclude of includeGraph.parentIncludesByTargetKey.get(ownerKey) ?? []) {
      if (visited.has(parentInclude.ownerKey)) {
        continue;
      }
      visited.add(parentInclude.ownerKey);
      const visible = isImplicitGlobalDeclarationVisibleFromFileAt(
        includeGraph,
        parentInclude.ownerKey,
        declaration,
        declarationKey,
        parentInclude.range,
        reachability,
        visibilityMemo,
        visited,
      );
      visited.delete(parentInclude.ownerKey);
      if (visible) {
        return true;
      }
    }
    return false;
  });
}

function hasEarlierReachableImplicitGlobalInclude(
  includeGraph: ImplicitGlobalIncludeGraph,
  ownerKey: string,
  targetKey: string,
  referenceRange: Range,
  reachability?: PrecomputedIncludeReachability,
): boolean {
  return (includeGraph.directIncludesByOwnerKey.get(ownerKey) ?? []).some((include) => {
    if (!positionBeforeOrEqual(include.range.start, referenceRange.start)) {
      return false;
    }
    if (include.targetKey === targetKey) {
      return true;
    }
    const precomputed = precomputedIncludeCanReachTarget(
      reachability,
      include.targetKey,
      targetKey,
    );
    return (
      precomputed === true ||
      (precomputed === undefined &&
        isImplicitGlobalIncludeReachable(
          includeGraph,
          include.targetKey,
          targetKey,
          new Set([ownerKey]),
        ))
    );
  });
}

function isImplicitGlobalIncludeReachable(
  includeGraph: ImplicitGlobalIncludeGraph,
  startKey: string,
  targetKey: string,
  visited: Set<string>,
): boolean {
  if (startKey === targetKey) {
    return true;
  }
  if (visited.has(startKey)) {
    return false;
  }
  visited.add(startKey);
  return (includeGraph.directIncludesByOwnerKey.get(startKey) ?? []).some(
    (include) =>
      include.targetKey === targetKey ||
      isImplicitGlobalIncludeReachable(includeGraph, include.targetKey, targetKey, visited),
  );
}

function precomputeIncludeReachability(
  graph: IncludeReachabilityGraph,
  targetKeys: Iterable<string>,
): PrecomputedIncludeReachability {
  const hasCycle = includeGraphHasCycle(graph);
  const reachingFileKeysByTarget = new Map<string, Set<string>>();
  if (hasCycle) {
    return { hasCycle, reachingFileKeysByTarget };
  }
  for (const targetKey of targetKeys) {
    const reaching = new Set<string>();
    const queue = [targetKey];
    for (let index = 0; index < queue.length; index += 1) {
      const currentKey = queue[index];
      for (const parentInclude of graph.parentIncludesByTargetKey.get(currentKey) ?? []) {
        if (reaching.has(parentInclude.ownerKey)) {
          continue;
        }
        reaching.add(parentInclude.ownerKey);
        queue.push(parentInclude.ownerKey);
      }
    }
    reachingFileKeysByTarget.set(targetKey, reaching);
  }
  return { hasCycle, reachingFileKeysByTarget };
}

function includeGraphHasCycle(graph: IncludeReachabilityGraph): boolean {
  const visiting = new Set<string>();
  const visited = new Set<string>();
  const visit = (ownerKey: string): boolean => {
    if (visiting.has(ownerKey)) {
      return true;
    }
    if (visited.has(ownerKey)) {
      return false;
    }
    visiting.add(ownerKey);
    for (const include of graph.directIncludesByOwnerKey.get(ownerKey) ?? []) {
      if (visit(include.targetKey)) {
        return true;
      }
    }
    visiting.delete(ownerKey);
    visited.add(ownerKey);
    return false;
  };
  for (const ownerKey of graph.directIncludesByOwnerKey.keys()) {
    if (visit(ownerKey)) {
      return true;
    }
  }
  return false;
}

function precomputedIncludeCanReachTarget(
  reachability: PrecomputedIncludeReachability | undefined,
  startKey: string,
  targetKey: string,
): boolean | undefined {
  if (!reachability || reachability.hasCycle) {
    return undefined;
  }
  return reachability.reachingFileKeysByTarget.get(targetKey)?.has(startKey) === true;
}

function implicitGlobalGroupSummary<T>(groups: Iterable<readonly T[]>): {
  groups: number;
  maxGroupSize: number;
} {
  let groupCount = 0;
  let maxGroupSize = 0;
  for (const group of groups) {
    groupCount += 1;
    maxGroupSize = Math.max(maxGroupSize, group.length);
  }
  return { groups: groupCount, maxGroupSize };
}

function pushMapItem<K, V>(map: Map<K, V[]>, key: K, value: V): void {
  const existing = map.get(key);
  if (existing) {
    existing.push(value);
  } else {
    map.set(key, [value]);
  }
}

function positionBeforeOrEqual(left: Position, right: Position): boolean {
  return left.line < right.line || (left.line === right.line && left.character <= right.character);
}

function throwIfCancelled(cancellation: AnalysisCancellation): void {
  if (cancellation.isCancellationRequested()) {
    throw new Error("Graph generation was cancelled.");
  }
}
