package lspserver

import (
	"maps"
	"math"
	"strings"
	"sync"
	"unsafe"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

type analysisCache struct {
	mu                 sync.RWMutex
	declarations       map[*core.ParsedDocument][]vbUsageDeclaration
	snapshots          map[*core.ParsedDocument]*fileAnalysisSnapshot
	workspaceSnapshots map[workspaceArtifactSnapshotCacheKey]*workspaceArtifactSnapshot
}

// addAnalysisCacheBytes adds one estimated component without allowing a
// malformed or overflowing estimate to make the aggregate negative. Cache
// estimators are advisory, so non-positive components do not contribute.
func addAnalysisCacheBytes(total, component int64) int64 {
	if total < 0 {
		total = 0
	}
	if component < 0 {
		component = 0
	}
	if component > math.MaxInt64-total {
		return math.MaxInt64
	}
	return total + component
}

func nonNegativeAnalysisCacheBytes(bytes int64) int64 {
	if bytes < 0 {
		return 0
	}
	return bytes
}

func newAnalysisCache() *analysisCache {
	return &analysisCache{
		declarations:       map[*core.ParsedDocument][]vbUsageDeclaration{},
		snapshots:          map[*core.ParsedDocument]*fileAnalysisSnapshot{},
		workspaceSnapshots: map[workspaceArtifactSnapshotCacheKey]*workspaceArtifactSnapshot{},
	}
}

// workspaceArtifactSnapshotCacheKey keeps the reduced workspace artifact
// cache separate from the complete file analysis snapshot cache. Parsed
// documents are immutable source revisions; the include fingerprint accounts
// for filesystem resolution changing without a source revision.
type workspaceArtifactSnapshotCacheKey struct {
	parsed                       *core.ParsedDocument
	includeResolutionFingerprint string
}

func (c *analysisCache) snapshot(parsed *core.ParsedDocument) *fileAnalysisSnapshot {
	c.mu.RLock()
	snapshot := c.snapshots[parsed]
	c.mu.RUnlock()
	return snapshot
}

func (c *analysisCache) rememberSnapshot(parsed *core.ParsedDocument, snapshot *fileAnalysisSnapshot) {
	if parsed == nil || snapshot == nil {
		return
	}
	c.mu.Lock()
	c.snapshots[parsed] = snapshot
	c.declarations[parsed] = snapshot.GraphDeclarations
	c.mu.Unlock()
}

func (c *analysisCache) workspaceSnapshot(parsed *core.ParsedDocument, includeResolutionFingerprint string) *workspaceArtifactSnapshot {
	if c == nil || parsed == nil {
		return nil
	}
	c.mu.RLock()
	snapshot := c.workspaceSnapshots[workspaceArtifactSnapshotCacheKey{
		parsed:                       parsed,
		includeResolutionFingerprint: includeResolutionFingerprint,
	}]
	c.mu.RUnlock()
	return snapshot
}

func (c *analysisCache) rememberWorkspaceSnapshot(parsed *core.ParsedDocument, includeResolutionFingerprint string, snapshot *workspaceArtifactSnapshot) {
	if c == nil || parsed == nil || snapshot == nil {
		return
	}
	c.mu.Lock()
	c.workspaceSnapshots[workspaceArtifactSnapshotCacheKey{
		parsed:                       parsed,
		includeResolutionFingerprint: includeResolutionFingerprint,
	}] = snapshot
	c.mu.Unlock()
}

func (c *analysisCache) vbDeclarations(parsed *core.ParsedDocument) []vbUsageDeclaration {
	if parsed == nil {
		return nil
	}
	c.mu.RLock()
	declarations, ok := c.declarations[parsed]
	c.mu.RUnlock()
	if ok {
		return declarations
	}
	declarations = graphVBDeclarations(parsed)
	c.mu.Lock()
	if cached, ok := c.declarations[parsed]; ok {
		declarations = cached
	} else {
		c.declarations[parsed] = declarations
	}
	c.mu.Unlock()
	return declarations
}

func (c *analysisCache) clear() {
	c.mu.Lock()
	c.declarations = map[*core.ParsedDocument][]vbUsageDeclaration{}
	c.snapshots = map[*core.ParsedDocument]*fileAnalysisSnapshot{}
	c.workspaceSnapshots = map[workspaceArtifactSnapshotCacheKey]*workspaceArtifactSnapshot{}
	c.mu.Unlock()
}

func (c *analysisCache) memoryEstimate() (int64, int) {
	return c.memoryEstimateWithExternalParsed(nil)
}

// memoryEstimateWithExternalParsed expects external to remain stable for the
// duration of the estimate. Server-owned callers hold Server.mu while taking
// the parsed-cache ownership snapshot and running this method.
func (c *analysisCache) memoryEstimateWithExternalParsed(external map[*core.ParsedDocument]struct{}) (int64, int) {
	if c == nil {
		return 0, 0
	}
	// Hold the cache lock only to snapshot its maps. Collecting the owners of
	// every external parsed document is the slow part, and holding the lock
	// through it blocked writers such as edits for the whole pressure check.
	c.mu.RLock()
	cachedDeclarations := maps.Clone(c.declarations)
	cachedSnapshots := maps.Clone(c.snapshots)
	cachedWorkspaceSnapshots := maps.Clone(c.workspaceSnapshots)
	c.mu.RUnlock()
	var bytes int64
	declarationOwners := make(map[*vbUsageDeclaration]struct{}, len(cachedDeclarations))
	parsedOwners := make(map[*core.ParsedDocument]struct{}, len(cachedDeclarations)+len(cachedSnapshots)+len(cachedWorkspaceSnapshots))
	for parsed, declarations := range cachedDeclarations {
		parsedOwners[parsed] = struct{}{}
		backing := unsafe.SliceData(declarations)
		if backing == nil {
			bytes = addAnalysisCacheBytes(bytes, estimateAnalysisCacheDeclarationsBytes(declarations))
			continue
		}
		if _, exists := declarationOwners[backing]; exists {
			bytes = addAnalysisCacheBytes(bytes, estimateAnalysisCacheDeclarationsBytes(declarations))
			continue
		}
		declarationOwners[backing] = struct{}{}
		bytes = addAnalysisCacheBytes(bytes, estimateAnalysisDeclarationStorageBytes(nil, declarations))
	}
	fileSnapshots := make(map[*fileAnalysisSnapshot]analysisSnapshotOwner, len(cachedSnapshots))
	for parsed, snapshot := range cachedSnapshots {
		parsedOwners[parsed] = struct{}{}
		if snapshot == nil {
			continue
		}
		owner := fileSnapshots[snapshot]
		owner.count++
		estimate := nonNegativeAnalysisCacheBytes(estimateAnalysisCacheFileSnapshotBytes(parsed, snapshot))
		if estimate > owner.bytes {
			owner.bytes = estimate
		}
		fileSnapshots[snapshot] = owner
	}
	for _, owner := range fileSnapshots {
		bytes = addAnalysisCacheBytes(bytes, owner.bytes)
	}
	workspaceSnapshots := make(map[*workspaceArtifactSnapshot]analysisSnapshotOwner, len(cachedWorkspaceSnapshots))
	for key, snapshot := range cachedWorkspaceSnapshots {
		parsedOwners[key.parsed] = struct{}{}
		if snapshot == nil {
			continue
		}
		owner := workspaceSnapshots[snapshot]
		owner.count++
		estimate := nonNegativeAnalysisCacheBytes(estimateAnalysisCacheWorkspaceSnapshotBytes(key.parsed, snapshot))
		if estimate > owner.bytes {
			owner.bytes = estimate
		}
		workspaceSnapshots[snapshot] = owner
	}
	for _, owner := range workspaceSnapshots {
		bytes = addAnalysisCacheBytes(bytes, owner.bytes)
	}
	runtimeOwners := runtimeAnalysisOwnerSet(external)
	structuralOwners := structuralAnalysisOwnerSet(external)
	for parsed := range parsedOwners {
		if parsed == nil {
			continue
		}
		if _, shared := external[parsed]; !shared {
			bytes = addAnalysisCacheBytes(bytes, parsed.EstimateStructuralBytesWithoutRevisionText())
			for _, owner := range parsed.StructuralMemoryOwners() {
				if _, exists := structuralOwners[owner.Identity]; exists {
					continue
				}
				structuralOwners[owner.Identity] = struct{}{}
				bytes = addAnalysisCacheBytes(bytes, owner.Bytes)
			}
			for _, owner := range parsed.RuntimeAnalysisMemoryOwners() {
				if _, exists := runtimeOwners[owner.Identity]; exists {
					continue
				}
				runtimeOwners[owner.Identity] = struct{}{}
				bytes = addAnalysisCacheBytes(bytes, owner.Bytes)
			}
		}
	}
	return bytes, len(cachedDeclarations) + len(cachedSnapshots) + len(cachedWorkspaceSnapshots)
}

type analysisSnapshotOwner struct {
	count int
	bytes int64
}

func (c *analysisCache) evict(target int64) int64 {
	return c.evictWithExternalParsed(target, nil)
}

// evictWithExternalParsed follows the same ownership-snapshot contract as
// memoryEstimateWithExternalParsed so a parsed revision cannot change owners
// between accounting and eviction.
func (c *analysisCache) evictWithExternalParsed(target int64, external map[*core.ParsedDocument]struct{}) int64 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var freed int64
	type declarationOwner struct {
		count   int
		payload int64
	}
	declarationOwners := make(map[*vbUsageDeclaration]declarationOwner, len(c.declarations))
	for _, declarations := range c.declarations {
		backing := unsafe.SliceData(declarations)
		if backing == nil {
			continue
		}
		owner := declarationOwners[backing]
		owner.count++
		storage := nonNegativeAnalysisCacheBytes(estimateAnalysisDeclarationStorageBytes(nil, declarations))
		header := nonNegativeAnalysisCacheBytes(estimateAnalysisCacheDeclarationsBytes(declarations))
		payload := int64(0)
		if storage > header {
			payload = storage - header
		}
		if payload > owner.payload {
			owner.payload = payload
		}
		declarationOwners[backing] = owner
	}
	releaseDeclarations := func(declarations []vbUsageDeclaration) {
		freed = addAnalysisCacheBytes(freed, estimateAnalysisCacheDeclarationsBytes(declarations))
		backing := unsafe.SliceData(declarations)
		if backing == nil {
			return
		}
		owner := declarationOwners[backing]
		owner.count--
		if owner.count > 0 {
			declarationOwners[backing] = owner
			return
		}
		delete(declarationOwners, backing)
		freed = addAnalysisCacheBytes(freed, owner.payload)
	}
	parsedOwners := make(map[*core.ParsedDocument]int, len(c.declarations)+len(c.snapshots)+len(c.workspaceSnapshots))
	for parsed := range c.declarations {
		parsedOwners[parsed]++
	}
	for parsed := range c.snapshots {
		parsedOwners[parsed]++
	}
	for key := range c.workspaceSnapshots {
		parsedOwners[key.parsed]++
	}
	type runtimeOwner struct {
		count int
		bytes int64
	}
	runtimeOwners := make(map[any]runtimeOwner)
	structuralOwners := make(map[any]runtimeOwner)
	for parsed := range parsedOwners {
		if parsed == nil {
			continue
		}
		if _, shared := external[parsed]; !shared {
			for _, owner := range parsed.StructuralMemoryOwners() {
				current := structuralOwners[owner.Identity]
				current.count++
				ownerBytes := nonNegativeAnalysisCacheBytes(owner.Bytes)
				if ownerBytes > current.bytes {
					current.bytes = ownerBytes
				}
				structuralOwners[owner.Identity] = current
			}
		}
		for _, owner := range parsed.RuntimeAnalysisMemoryOwners() {
			current := runtimeOwners[owner.Identity]
			current.count++
			ownerBytes := nonNegativeAnalysisCacheBytes(owner.Bytes)
			if ownerBytes > current.bytes {
				current.bytes = ownerBytes
			}
			runtimeOwners[owner.Identity] = current
		}
	}
	externalRuntimeOwners := runtimeAnalysisOwnerSet(external)
	externalStructuralOwners := structuralAnalysisOwnerSet(external)
	releaseParsed := func(parsed *core.ParsedDocument) {
		if parsed == nil {
			return
		}
		remaining := parsedOwners[parsed] - 1
		if remaining > 0 {
			parsedOwners[parsed] = remaining
			return
		}
		delete(parsedOwners, parsed)
		if _, shared := external[parsed]; !shared {
			freed = addAnalysisCacheBytes(freed, parsed.EstimateStructuralBytesWithoutRevisionText())
			for _, owner := range parsed.StructuralMemoryOwners() {
				current := structuralOwners[owner.Identity]
				current.count--
				if current.count > 0 {
					structuralOwners[owner.Identity] = current
					continue
				}
				delete(structuralOwners, owner.Identity)
				if _, retained := externalStructuralOwners[owner.Identity]; !retained {
					freed = addAnalysisCacheBytes(freed, current.bytes)
				}
			}
			for _, owner := range parsed.RuntimeAnalysisMemoryOwners() {
				current := runtimeOwners[owner.Identity]
				current.count--
				if current.count > 0 {
					runtimeOwners[owner.Identity] = current
					continue
				}
				delete(runtimeOwners, owner.Identity)
				if _, retained := externalRuntimeOwners[owner.Identity]; !retained {
					freed = addAnalysisCacheBytes(freed, current.bytes)
				}
			}
		}
	}
	fileSnapshotOwners := make(map[*fileAnalysisSnapshot]analysisSnapshotOwner, len(c.snapshots))
	for parsed, snapshot := range c.snapshots {
		if snapshot != nil {
			owner := fileSnapshotOwners[snapshot]
			owner.count++
			estimate := nonNegativeAnalysisCacheBytes(estimateAnalysisCacheFileSnapshotBytes(parsed, snapshot))
			if estimate > owner.bytes {
				owner.bytes = estimate
			}
			fileSnapshotOwners[snapshot] = owner
		}
	}
	workspaceSnapshotOwners := make(map[*workspaceArtifactSnapshot]analysisSnapshotOwner, len(c.workspaceSnapshots))
	for key, snapshot := range c.workspaceSnapshots {
		if snapshot != nil {
			owner := workspaceSnapshotOwners[snapshot]
			owner.count++
			estimate := nonNegativeAnalysisCacheBytes(estimateAnalysisCacheWorkspaceSnapshotBytes(key.parsed, snapshot))
			if estimate > owner.bytes {
				owner.bytes = estimate
			}
			workspaceSnapshotOwners[snapshot] = owner
		}
	}
	releaseFileSnapshot := func(snapshot *fileAnalysisSnapshot) {
		if snapshot == nil {
			return
		}
		owner, ok := fileSnapshotOwners[snapshot]
		if !ok {
			return
		}
		owner.count--
		if owner.count > 0 {
			fileSnapshotOwners[snapshot] = owner
			return
		}
		delete(fileSnapshotOwners, snapshot)
		freed = addAnalysisCacheBytes(freed, owner.bytes)
	}
	releaseWorkspaceSnapshot := func(snapshot *workspaceArtifactSnapshot) {
		if snapshot == nil {
			return
		}
		owner, ok := workspaceSnapshotOwners[snapshot]
		if !ok {
			return
		}
		owner.count--
		if owner.count > 0 {
			workspaceSnapshotOwners[snapshot] = owner
			return
		}
		delete(workspaceSnapshotOwners, snapshot)
		freed = addAnalysisCacheBytes(freed, owner.bytes)
	}
	for parsed, snapshot := range c.snapshots {
		if target > 0 && freed >= target {
			break
		}
		delete(c.snapshots, parsed)
		releaseFileSnapshot(snapshot)
		releaseParsed(parsed)
	}
	for parsed := range c.declarations {
		if target > 0 && freed >= target {
			break
		}
		releaseDeclarations(c.declarations[parsed])
		delete(c.declarations, parsed)
		releaseParsed(parsed)
	}
	for key, snapshot := range c.workspaceSnapshots {
		if target > 0 && freed >= target {
			break
		}
		delete(c.workspaceSnapshots, key)
		releaseWorkspaceSnapshot(snapshot)
		releaseParsed(key.parsed)
	}
	return freed
}

func runtimeAnalysisOwnerSet(parsedDocuments map[*core.ParsedDocument]struct{}) map[any]struct{} {
	return analysisOwnerSet(parsedDocuments, (*core.ParsedDocument).RuntimeAnalysisMemoryOwners)
}

func structuralAnalysisOwnerSet(parsedDocuments map[*core.ParsedDocument]struct{}) map[any]struct{} {
	return analysisOwnerSet(parsedDocuments, (*core.ParsedDocument).StructuralMemoryOwners)
}

// analysisOwnerSet sizes the set before filling it; these sets cover every
// cached document and rehashing dominated their cost under memory pressure.
func analysisOwnerSet(parsedDocuments map[*core.ParsedDocument]struct{}, ownersOf func(*core.ParsedDocument) []core.RuntimeAnalysisMemoryOwner) map[any]struct{} {
	lists := make([][]core.RuntimeAnalysisMemoryOwner, 0, len(parsedDocuments))
	total := 0
	for parsed := range parsedDocuments {
		if parsed == nil {
			continue
		}
		owners := ownersOf(parsed)
		lists = append(lists, owners)
		total += len(owners)
	}
	set := make(map[any]struct{}, total)
	for _, owners := range lists {
		for _, owner := range owners {
			set[owner.Identity] = struct{}{}
		}
	}
	return set
}

func estimateAnalysisDeclarationsBytes(parsed *core.ParsedDocument, declarations []vbUsageDeclaration) int64 {
	var bytes int64 = 64
	if parsed != nil {
		bytes += int64(len(parsed.URI))*2 + 16
	}
	for _, declaration := range declarations {
		bytes += int64(len(declaration.Name)+len(declaration.Kind)+len(declaration.Scope)+len(declaration.MemberOf)+len(declaration.AssignedValue)+len(declaration.TypeName)+len(declaration.ProcedureKind))*2 + 160
	}
	return bytes
}

func estimateAnalysisDeclarationStorageBytes(parsed *core.ParsedDocument, declarations []vbUsageDeclaration) int64 {
	bytes := estimateAnalysisDeclarationsBytes(parsed, declarations)
	bytes += int64(cap(declarations)-len(declarations)) * 160
	return bytes
}

// Snapshot estimators charge only backing unique to each cache layer. The
// caller separately assigns ParsedDocument ownership to parsedCache while a
// revision is current, or to analysisCache after that revision is replaced.
// Structural source-text owners are deduplicated across both ownership sets.
func estimateAnalysisCacheDeclarationsBytes(_ []vbUsageDeclaration) int64 {
	// The declarations map owns the decoded slice. Shared backing is charged
	// once by memoryEstimateWithExternalParsed; every entry keeps this header.
	return 64
}

func estimateAnalysisCacheFileSnapshotBytes(parsed *core.ParsedDocument, snapshot *fileAnalysisSnapshot) int64 {
	if snapshot == nil {
		return 0
	}
	if !snapshot.runtimeBacked {
		return estimateRestoredFileAnalysisSnapshotBytes(snapshot)
	}
	bytes := int64(512)
	// The complete live snapshot shares decoded reference facts, symbols,
	// signatures, usage, graph data, summary, document metadata, and the
	// reference shard with ParsedDocument runtime analysis. It uniquely owns
	// only its assembled symbol facts, include-resolution slice, outer virtual
	// document map, and any synthetic virtual payload that does not share
	// runtime backing.
	bytes += estimateAnalysisCacheIncludesBytes(snapshot.Includes)
	bytes += estimateAnalysisCacheSymbolFactsBytes(snapshot.SymbolFacts)
	bytes += estimateAnalysisCacheVirtualDocumentsBytes(parsed, snapshot.VirtualDocuments)
	// These producers persist JSON into ParsedDocument.Analysis but return
	// separate decoded collections. The live snapshot is their only typed owner.
	bytes += estimateVBReferenceDocumentFactsBytes(snapshot.ReferenceFacts)
	bytes += estimateVBMemberOccurrencesBytes(snapshot.Members)
	bytes += estimateVBUsageDeclarationListBytes(snapshot.NamingDeclarations)
	bytes += int64(cap(snapshot.VBDocumentSymbols))*192 + int64(len(snapshot.VBDocumentSymbols))*64
	bytes += int64(cap(snapshot.VBFoldingRanges)) * 48
	bytes += int64(cap(snapshot.DocumentColors)) * 64
	bytes += int64(len(snapshot.VBClassLines)+len(snapshot.VBProcedureLines)) * 24
	return bytes
}

func estimateRestoredFileAnalysisSnapshotBytes(snapshot *fileAnalysisSnapshot) int64 {
	// JSON restoration decodes a fresh value for every persisted field. Seeded
	// ReferenceShard and SymbolIndex are the only exceptions: they are installed
	// into ParsedDocument runtime analysis and remain owned by that parsed
	// revision rather than by this snapshot.
	// GraphDeclarations are charged to the declarations cache entry because
	// that entry keeps the decoded slice alive after this snapshot is removed.
	return estimateFileAnalysisSnapshotStorageBytes(snapshot, false, false, false)
}

func estimateAnalysisCacheWorkspaceSnapshotBytes(parsed *core.ParsedDocument, snapshot *workspaceArtifactSnapshot) int64 {
	if snapshot == nil {
		return 0
	}
	bytes := int64(256)
	bytes += estimateAnalysisCacheIncludesBytes(snapshot.Includes)
	bytes += estimateAnalysisCacheVirtualDocumentsBytes(parsed, snapshot.VirtualDocuments)
	return bytes
}

func estimateAnalysisCacheIncludesBytes(includes []resolvedIncludeSnapshot) int64 {
	return int64(cap(includes)) * 128
}

func estimateAnalysisCacheSymbolFactsBytes(values map[string]symbolAnalysisFact) int64 {
	if len(values) == 0 {
		return 0
	}
	bytes := int64(64 + len(values)*192)
	for _, fact := range values {
		bytes += int64(cap(fact.Occurrences)) * 48
	}
	return bytes
}

func estimateAnalysisCacheVirtualDocumentsBytes(parsed *core.ParsedDocument, values map[core.EmbeddedLanguage]core.VirtualDocument) int64 {
	if len(values) == 0 {
		return 0
	}
	bytes := int64(64 + len(values)*128)
	for language, virtual := range values {
		runtimeVirtual, runtimeBacked := analysisCacheRuntimeVirtualDocument(parsed, language)
		if len(virtual.Text) > 0 && (!runtimeBacked || !analysisCacheVirtualDocumentStringBackingShared(virtual.Text, runtimeVirtual.Text)) {
			bytes += int64(len(virtual.Text))*2 + 16
		}
		if cap(virtual.Segments) > 0 && (!runtimeBacked || !analysisCacheVirtualDocumentSliceBackingShared(virtual.Segments, runtimeVirtual.Segments)) {
			bytes += int64(cap(virtual.Segments)) * 48
		}
	}
	return bytes
}

func analysisCacheRuntimeVirtualDocument(parsed *core.ParsedDocument, language core.EmbeddedLanguage) (core.VirtualDocument, bool) {
	if parsed == nil {
		return core.VirtualDocument{}, false
	}
	runtimeKey := "core.virtual-document.runtime.v1." + string(language)
	cached, ok := parsed.LoadRuntimeAnalysis(runtimeKey)
	if !ok {
		return core.VirtualDocument{}, false
	}
	virtual, ok := cached.(core.VirtualDocument)
	return virtual, ok
}

func analysisCacheVirtualDocumentStringBackingShared(first, second string) bool {
	if len(first) == 0 || len(second) == 0 {
		return len(first) == 0 && len(second) == 0
	}
	return unsafe.StringData(first) == unsafe.StringData(second)
}

func analysisCacheVirtualDocumentSliceBackingShared(first, second []core.SourceMapSegment) bool {
	if cap(first) == 0 || cap(second) == 0 {
		return cap(first) == 0 && cap(second) == 0
	}
	return unsafe.SliceData(first) == unsafe.SliceData(second)
}

// estimateFileAnalysisSnapshotBytes reports the complete decoded snapshot
// payload. Cache-specific estimators use narrower ownership rules when a live
// snapshot shares values with ParsedDocument or a restored snapshot seeds
// selected values into ParsedDocument runtime analysis.
func estimateFileAnalysisSnapshotBytes(snapshot *fileAnalysisSnapshot) int64 {
	if snapshot == nil {
		return 0
	}
	return estimateFileAnalysisSnapshotStorageBytes(snapshot, true, true, true)
}

func estimateFileAnalysisSnapshotStorageBytes(snapshot *fileAnalysisSnapshot, includeSymbols, includeReferenceShard, includeGraphDeclarations bool) int64 {
	if snapshot == nil {
		return 0
	}
	bytes := int64(len(snapshot.URI)+len(snapshot.IncludeResolutionFingerprint))*2 + 512
	bytes += estimateVBReferenceDocumentFactsBytes(snapshot.ReferenceFacts)
	bytes += estimateVBScriptSignatureMapBytes(snapshot.Signatures)
	bytes += estimateAnalysisCacheSymbolFactsBytes(snapshot.SymbolFacts)
	if includeSymbols {
		bytes += estimateVBScriptSymbolIndexBytes(snapshot.Symbols)
	}
	bytes += estimateVBScriptSignaturesBytes(snapshot.SignatureList)
	bytes += estimateVBAssignmentsBytes(snapshot.Assignments)
	bytes += estimateVBMemberOccurrencesBytes(snapshot.Members)
	bytes += estimateResolvedIncludeSnapshotBytes(snapshot.Includes)
	bytes += estimateVBUsageDeclarationListBytes(snapshot.Usage.Declarations)
	bytes += estimateVBUsageDeclarationListBytes(snapshot.NamingDeclarations)
	if includeGraphDeclarations {
		bytes += estimateVBUsageDeclarationListBytes(snapshot.GraphDeclarations)
	}
	bytes += int64(cap(snapshot.VBDocumentSymbols))*192 + int64(len(snapshot.VBDocumentSymbols))*64
	bytes += int64(cap(snapshot.VBFoldingRanges)) * 48
	bytes += int64(cap(snapshot.DocumentColors)) * 64
	bytes += int64(len(snapshot.VBClassLines)+len(snapshot.VBProcedureLines)) * 24
	bytes += estimateVBFileAnalysisSummaryBytes(snapshot.Summary)
	bytes += estimateVBGraphAnalysisTypesBytes(snapshot.AnalysisTypes)
	if includeReferenceShard {
		bytes += snapshot.ReferenceShard.EstimateBytes()
	}
	bytes += estimateVirtualDocumentStorageBytes(snapshot.VirtualDocuments)
	return bytes
}

func estimateVBReferenceDocumentFactsBytes(facts vbReferenceDocumentFacts) int64 {
	bytes := int64(64)
	bytes += estimateVBReferenceRangeMapBytes(facts.DeclarationRanges)
	bytes += estimateVBReferenceRangeMapBytes(facts.ObjectInitializationRanges)
	return bytes
}

func estimateVBReferenceRangeMapBytes(values map[string][]lsp.Range) int64 {
	if len(values) == 0 {
		return 0
	}
	bytes := int64(64 + len(values)*96)
	for name, ranges := range values {
		bytes += int64(len(name))*2 + int64(cap(ranges))*32
	}
	return bytes
}

func estimateVBScriptSymbolIndexBytes(index vbscript.SymbolIndex) int64 {
	bytes := int64(64 + len(index.Declarations)*192 + len(index.Occurrences)*96)
	for name, symbol := range index.Declarations {
		bytes += int64(len(name)+len(symbol.Name)+len(symbol.Kind)) * 2
	}
	for name, occurrences := range index.Occurrences {
		bytes += int64(len(name))*2 + int64(cap(occurrences))*128
	}
	return bytes
}

func estimateVBScriptSignatureMapBytes(values map[string]vbscript.Signature) int64 {
	if len(values) == 0 {
		return 0
	}
	bytes := int64(64 + len(values)*160)
	for name, signature := range values {
		bytes += int64(len(name))*2 + estimateVBScriptSignatureBytes(signature)
	}
	return bytes
}

func estimateVBScriptSignaturesBytes(values []vbscript.Signature) int64 {
	bytes := int64(cap(values)) * 96
	for _, signature := range values {
		bytes += estimateVBScriptSignatureBytes(signature)
	}
	return bytes
}

func estimateVBAssignmentsBytes(values []vbAssignment) int64 {
	bytes := int64(cap(values)) * 96
	for _, assignment := range values {
		bytes += int64(160+len(assignment.Name)+len(assignment.Scope)) * 2
	}
	return bytes
}

func estimateVBMemberOccurrencesBytes(values []graphMemberOccurrence) int64 {
	bytes := int64(cap(values)) * 96
	for _, occurrence := range values {
		bytes += int64(128+len(occurrence.URI)+len(occurrence.FullPath)+len(occurrence.ReceiverName)+len(occurrence.MemberName)) * 2
		bytes += int64(cap(occurrence.Parts)) * 16
		for _, part := range occurrence.Parts {
			bytes += int64(len(part)) * 2
		}
	}
	return bytes
}

func estimateVBUsageDeclarationListBytes(values []vbUsageDeclaration) int64 {
	bytes := estimateAnalysisDeclarationStorageBytes(nil, values)
	return bytes
}

func estimateVirtualDocumentStorageBytes(values map[core.EmbeddedLanguage]core.VirtualDocument) int64 {
	if len(values) == 0 {
		return 0
	}
	bytes := int64(64 + len(values)*128)
	for language, virtual := range values {
		bytes += int64(len(language)+len(virtual.URI)+len(virtual.LanguageID)+len(virtual.Text))*2 + int64(cap(virtual.Segments))*48 + 96
	}
	return bytes
}

// estimateVBGraphAnalysisTypesBytes charges the decoded graph type maps owned
// by a complete file snapshot. Map capacity is not observable, so each map
// entry includes a conservative allowance for its bucket and value storage.
func estimateVBGraphAnalysisTypesBytes(analysis vbGraphAnalysisTypes) int64 {
	bytes := int64(128)
	bytes += estimateAnalysisTypeStringMapBytes(analysis.Types)
	bytes += estimateAnalysisTypeAnnotationsMapBytes(analysis.TypeAnnotations)
	bytes += estimateAnalysisTypeStringMapBytes(analysis.Returns)
	bytes += estimateAnalysisTypeNestedStringMapBytes(analysis.Params)
	bytes += estimateAnalysisTypeNestedStringMapBytes(analysis.Members)
	bytes += estimateAnalysisTypeSignatureMapBytes(analysis.Signatures)
	bytes += estimateAnalysisTypeStringMapBytes(analysis.ScopedReturns)
	bytes += estimateAnalysisTypeNestedStringMapBytes(analysis.ScopedParams)
	bytes += estimateAnalysisTypeSignatureMapBytes(analysis.ScopedSignatures)
	return bytes
}

func estimateAnalysisTypeStringMapBytes(values map[string]string) int64 {
	if len(values) == 0 {
		return 0
	}
	bytes := int64(64) + int64(len(values))*128
	for key, value := range values {
		bytes += int64(len(key)+len(value)) * 2
	}
	return bytes
}

func estimateAnalysisTypeAnnotationsMapBytes(values map[string][]vbTypeAnnotation) int64 {
	if len(values) == 0 {
		return 0
	}
	bytes := int64(64) + int64(len(values))*128
	for key, annotations := range values {
		bytes += int64(len(key)) * 2
		bytes += int64(cap(annotations)) * 128
		for _, annotation := range annotations {
			bytes += int64(len(annotation.TypeName)+len(annotation.Scope)+len(annotation.MemberOf)+len(annotation.Accessor)) * 2
		}
	}
	return bytes
}

func estimateAnalysisTypeNestedStringMapBytes(values map[string]map[string]string) int64 {
	if len(values) == 0 {
		return 0
	}
	bytes := int64(64) + int64(len(values))*128
	for key, nested := range values {
		bytes += int64(len(key)) * 2
		bytes += estimateAnalysisTypeStringMapBytes(nested)
	}
	return bytes
}

func estimateAnalysisTypeSignatureMapBytes(values map[string]vbscript.Signature) int64 {
	if len(values) == 0 {
		return 0
	}
	bytes := int64(64) + int64(len(values))*160
	for key, signature := range values {
		bytes += int64(len(key)) * 2
		bytes += estimateVBScriptSignatureBytes(signature)
	}
	return bytes
}

func estimateVBScriptSignatureBytes(signature vbscript.Signature) int64 {
	bytes := int64(128) + int64(len(signature.Name)+len(signature.Kind)+len(signature.Label))*2
	bytes += int64(cap(signature.Parameters)) * 48
	for _, parameter := range signature.Parameters {
		bytes += int64(len(parameter.Name)+len(parameter.Mode)) * 2
	}
	return bytes
}

func (c *analysisCache) deleteURI(uri string) {
	c.mu.Lock()
	remove := func(parsed *core.ParsedDocument) {
		if parsed != nil && strings.EqualFold(parsed.URI, uri) {
			delete(c.snapshots, parsed)
			delete(c.declarations, parsed)
		}
	}
	for parsed := range c.snapshots {
		remove(parsed)
	}
	for parsed := range c.declarations {
		remove(parsed)
	}
	for key := range c.workspaceSnapshots {
		if key.parsed != nil && strings.EqualFold(key.parsed.URI, uri) {
			delete(c.workspaceSnapshots, key)
		}
	}
	c.mu.Unlock()
}

func (s *Server) cachedFileAnalysisSnapshot(parsed *core.ParsedDocument) *fileAnalysisSnapshot {
	if s == nil || s.analysisCache == nil || parsed == nil {
		return nil
	}
	return s.analysisCache.snapshot(parsed)
}

func (s *Server) rememberFileAnalysisSnapshot(parsed *core.ParsedDocument, snapshot *fileAnalysisSnapshot) {
	if s == nil || s.analysisCache == nil {
		return
	}
	s.analysisCache.rememberSnapshot(parsed, snapshot)
}

func (s *Server) cachedWorkspaceArtifactSnapshot(parsed *core.ParsedDocument, includeResolutionFingerprint string) *workspaceArtifactSnapshot {
	if s == nil || s.analysisCache == nil || parsed == nil {
		return nil
	}
	return s.analysisCache.workspaceSnapshot(parsed, includeResolutionFingerprint)
}

func (s *Server) rememberWorkspaceArtifactSnapshot(parsed *core.ParsedDocument, includeResolutionFingerprint string, snapshot *workspaceArtifactSnapshot) {
	if s == nil || s.analysisCache == nil {
		return
	}
	s.analysisCache.rememberWorkspaceSnapshot(parsed, includeResolutionFingerprint, snapshot)
}

func (s *Server) cachedVBDeclarations(parsed *core.ParsedDocument) []vbUsageDeclaration {
	if parsed == nil {
		return nil
	}
	if s == nil || s.analysisCache == nil {
		return graphVBDeclarations(parsed)
	}
	return s.analysisCache.vbDeclarations(parsed)
}

func (s *Server) clearAnalysisCache() {
	if s == nil || s.analysisCache == nil {
		return
	}
	s.analysisCache.clear()
}

func (s *Server) deleteAnalysisCacheForURI(uri string) {
	if s == nil || s.analysisCache == nil {
		return
	}
	s.analysisCache.deleteURI(uri)
}
