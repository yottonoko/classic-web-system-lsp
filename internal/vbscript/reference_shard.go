package vbscript

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

const referenceShardVersion = 3

// ReferenceShardVersion is the current persisted shard format.
const ReferenceShardVersion = referenceShardVersion

const referenceShardAnalysisKey = "vbscript.reference-shard.v3"

type referenceShardBuild struct {
	done  chan struct{}
	shard *ReferenceShard
}

var referenceShardBuilds sync.Map

// ReferenceRole describes how an identifier participates in a VBScript document.
type ReferenceRole uint16

const (
	// ReferenceRoleDeclaration marks a declaration name.
	ReferenceRoleDeclaration ReferenceRole = 1 << iota
	// ReferenceRoleRead marks a value or symbol read.
	ReferenceRoleRead
	// ReferenceRoleWrite marks an assignment target.
	ReferenceRoleWrite
	// ReferenceRoleCall marks a name followed by a call argument list.
	ReferenceRoleCall
	// ReferenceRoleFunctionReturn marks an assignment to the active function name.
	ReferenceRoleFunctionReturn
	// ReferenceRoleObjectInitialization marks an object-producing assignment or New type.
	ReferenceRoleObjectInitialization
	// ReferenceRoleCref marks a reference in an XML documentation cref attribute.
	ReferenceRoleCref
)

// ReferencePosting is one persistable identifier occurrence in a reference shard.
type ReferencePosting struct {
	Name       string
	Range      lsp.Range
	Roles      ReferenceRole
	Scope      string
	ScopeKind  string
	Owner      string
	ClassOwner string
}

// ReferenceTarget identifies the lexical binding selected by one posting.
type ReferenceTarget struct {
	Scope          string
	ScopeKind      string
	Owner          string
	ClassOwner     string
	ProcedureLocal bool
}

// HasRole reports whether the posting has every requested role bit.
func (posting ReferencePosting) HasRole(role ReferenceRole) bool {
	return posting.Roles&role == role
}

// PostingResolvesToGlobal reports whether an unqualified occurrence is not
// shadowed by a same-named procedure local or member of its enclosing class.
func PostingResolvesToGlobal(posting ReferencePosting, sameNamePostings []ReferencePosting) bool {
	if posting.Owner != "" {
		return false
	}
	for _, candidate := range sameNamePostings {
		if isProcedureLocalDeclarationPosting(candidate) && sameReferenceScope(posting, candidate) {
			return false
		}
	}
	if posting.ClassOwner == "" {
		return true
	}
	for _, candidate := range sameNamePostings {
		if isClassMemberDeclarationPosting(candidate) && strings.EqualFold(candidate.ClassOwner, posting.ClassOwner) {
			return false
		}
	}
	return true
}

type globalReferenceScopeKey struct {
	classOwner string
	scope      string
	scopeKind  string
}

type globalReferenceShadows struct {
	classMembers map[string]struct{}
	locals       map[globalReferenceScopeKey]struct{}
	foldKeys     map[string]string
}

func buildGlobalReferenceShadows(postings []ReferencePosting) globalReferenceShadows {
	shadows := globalReferenceShadows{}
	for _, posting := range postings {
		if !posting.HasRole(ReferenceRoleDeclaration) {
			continue
		}
		if isClassMemberDeclarationPosting(posting) {
			if shadows.classMembers == nil {
				shadows.classMembers = make(map[string]struct{})
			}
			shadows.classMembers[shadows.foldKey(posting.ClassOwner)] = struct{}{}
			continue
		}
		if !isProcedureLocalDeclarationPosting(posting) {
			continue
		}
		if shadows.locals == nil {
			shadows.locals = make(map[globalReferenceScopeKey]struct{})
		}
		shadows.locals[globalReferenceScopeKey{
			classOwner: shadows.foldKey(posting.ClassOwner),
			scope:      shadows.foldKey(posting.Scope),
			scopeKind:  posting.ScopeKind,
		}] = struct{}{}
	}
	return shadows
}

func postingResolvesToGlobalWithShadows(posting ReferencePosting, shadows *globalReferenceShadows) bool {
	if posting.Owner != "" {
		return false
	}
	classOwner := shadows.foldKey(posting.ClassOwner)
	if _, shadowed := shadows.locals[globalReferenceScopeKey{
		classOwner: classOwner,
		scope:      shadows.foldKey(posting.Scope),
		scopeKind:  posting.ScopeKind,
	}]; shadowed {
		return false
	}
	if posting.ClassOwner == "" {
		return true
	}
	_, shadowed := shadows.classMembers[classOwner]
	return !shadowed
}

func (shadows *globalReferenceShadows) foldKey(value string) string {
	if folded, ok := shadows.foldKeys[value]; ok {
		return folded
	}
	if shadows.foldKeys == nil {
		shadows.foldKeys = make(map[string]string)
	}
	folded := referenceFoldKey(value)
	shadows.foldKeys[value] = folded
	return folded
}

func referenceFoldKey(value string) string {
	return strings.Map(func(item rune) rune {
		canonical := item
		for folded := unicode.SimpleFold(item); folded != item; folded = unicode.SimpleFold(folded) {
			if folded > canonical {
				canonical = folded
			}
		}
		return canonical
	}, value)
}

// GlobalReferenceResolutions returns global-resolution decisions aligned with postings.
func GlobalReferenceResolutions(postings []ReferencePosting) []bool {
	if len(postings) == 0 {
		return nil
	}
	shadows := buildGlobalReferenceShadows(postings)
	resolved := make([]bool, len(postings))
	for index, posting := range postings {
		resolved[index] = postingResolvesToGlobalWithShadows(posting, &shadows)
	}
	return resolved
}

func isClassMemberDeclarationPosting(posting ReferencePosting) bool {
	if posting.ClassOwner == "" || !posting.HasRole(ReferenceRoleDeclaration) {
		return false
	}
	if posting.ScopeKind == "class" {
		return true
	}
	switch posting.ScopeKind {
	case "function", "sub", "property-get", "property-let", "property-set":
		return strings.EqualFold(posting.Name, posting.Scope)
	default:
		return false
	}
}

func isProcedureLocalDeclarationPosting(posting ReferencePosting) bool {
	return posting.HasRole(ReferenceRoleDeclaration) &&
		isProcedureScopeKind(posting.ScopeKind) &&
		!strings.EqualFold(posting.Name, posting.Scope)
}

func isProcedureScopeKind(kind string) bool {
	switch strings.ToLower(kind) {
	case "function", "sub", "property-get", "property-let", "property-set":
		return true
	default:
		return false
	}
}

// ReferenceScope describes the lexical VBScript class or procedure containing postings.
type ReferenceScope struct {
	Name  string
	Kind  string
	Range lsp.Range
}

// ReferenceRoleCounts contains the per-role totals for one normalized name.
type ReferenceRoleCounts struct {
	Declarations          int
	Reads                 int
	Writes                int
	Calls                 int
	FunctionReturns       int
	ObjectInitializations int
	Crefs                 int
}

// ReferenceCallGroup identifies call postings with the same normalized lexical context.
// PostingIndexes address the immutable slice returned by ReferenceShard.PostingsFor.
type ReferenceCallGroup struct {
	Scope          string
	ScopeKind      string
	Owner          string
	ClassOwner     string
	PostingIndexes []int
}

// ReferenceNameSummary contains immutable aggregate facts for one normalized name.
type ReferenceNameSummary struct {
	Postings            int
	Roles               ReferenceRoleCounts
	Declared            bool
	DeclarationRanges   []lsp.Range
	CallGroups          []ReferenceCallGroup
	GlobalResolutions   []bool
	CountFingerprint    string
	LocationFingerprint string
}

// ReferenceShard contains the source-derived reference facts for one parsed document.
// Its maps and slices are immutable after construction and safe to persist as JSON.
type ReferenceShard struct {
	Version      int
	Declarations map[string]Symbol
	Postings     map[string][]ReferencePosting
	Scopes       []ReferenceScope

	metadataOnce           sync.Once
	metadataMu             sync.RWMutex
	metadataReady          bool
	metadata               referenceShardMetadata
	documentCST            *CSTNode
	documentCSTEstimateFor *CSTNode
	documentCSTEstimate    int64
	documentCSTGeneration  uint64
	memoryGeneration       atomic.Uint64
}

type referenceShardMetadata struct {
	names     []string
	summaries map[string]ReferenceNameSummary
}

// PostingsFor returns the immutable postings for a case-insensitive identifier name.
func (shard *ReferenceShard) PostingsFor(name string) []ReferencePosting {
	if shard == nil {
		return nil
	}
	return shard.Postings[strings.ToLower(name)]
}

// ReferenceTargetAt returns the lexical binding selected by the named posting at position.
func (shard *ReferenceShard) ReferenceTargetAt(name string, position lsp.Position) (ReferenceTarget, bool) {
	postings := shard.PostingsFor(name)
	for _, posting := range postings {
		if !positionInLSPRange(position, posting.Range) {
			continue
		}
		return ReferenceTarget{
			Scope:          posting.Scope,
			ScopeKind:      posting.ScopeKind,
			Owner:          posting.Owner,
			ClassOwner:     posting.ClassOwner,
			ProcedureLocal: postingResolvesToProcedureLocal(posting, postings),
		}, true
	}
	return ReferenceTarget{}, false
}

func postingResolvesToProcedureLocal(posting ReferencePosting, sameNamePostings []ReferencePosting) bool {
	if posting.Owner != "" || !isProcedureScopeKind(posting.ScopeKind) {
		return false
	}
	for _, candidate := range sameNamePostings {
		if candidate.Owner != "" || !isProcedureLocalDeclarationPosting(candidate) {
			continue
		}
		if sameReferenceScope(posting, candidate) {
			return true
		}
	}
	return false
}

func sameReferenceScope(left ReferencePosting, right ReferencePosting) bool {
	return strings.EqualFold(left.ClassOwner, right.ClassOwner) &&
		strings.EqualFold(left.Scope, right.Scope) &&
		strings.EqualFold(left.ScopeKind, right.ScopeKind)
}

// NormalizedNames returns the immutable sorted normalized names in the shard.
func (shard *ReferenceShard) NormalizedNames() []string {
	if shard == nil {
		return nil
	}
	return shard.referenceMetadata().names
}

// SummaryFor returns aggregate facts and fingerprints for a case-insensitive name.
func (shard *ReferenceShard) SummaryFor(name string) (ReferenceNameSummary, bool) {
	if shard == nil {
		return ReferenceNameSummary{}, false
	}
	normalized := strings.ToLower(name)
	summary, ok := shard.referenceMetadata().summaries[normalized]
	return summary, ok
}

func (shard *ReferenceShard) referenceMetadata() *referenceShardMetadata {
	shard.metadataOnce.Do(func() {
		shard.metadataMu.Lock()
		defer shard.metadataMu.Unlock()
		shard.metadata.names = sortedReferenceShardNames(shard)
		shard.metadata.summaries = make(map[string]ReferenceNameSummary, len(shard.metadata.names))
		for _, name := range shard.metadata.names {
			declaration, declared := shard.Declarations[name]
			shard.metadata.summaries[name] = summarizeReferenceName(name, shard.Postings[name], declaration, declared)
		}
		shard.metadataReady = true
		shard.memoryGeneration.Add(1)
	})
	return &shard.metadata
}

// EstimateBytes reports an approximate number of bytes retained by the shard.
// Lazy metadata is included only after it has already been materialized.
func (shard *ReferenceShard) EstimateBytes() int64 {
	if shard == nil {
		return 0
	}
	bytes := int64(128 + len(shard.Declarations)*160)
	for name, declaration := range shard.Declarations {
		bytes += int64(len(name)+len(declaration.Name)+len(declaration.Kind)) * 2
	}
	for name, postings := range shard.Postings {
		bytes += int64(len(name))*2 + int64(cap(postings))*128 + 64
		for _, posting := range postings {
			bytes += int64(len(posting.Name)+len(posting.Scope)+len(posting.ScopeKind)+len(posting.Owner)+len(posting.ClassOwner)) * 2
		}
	}
	bytes += int64(cap(shard.Scopes)) * 96
	for _, scope := range shard.Scopes {
		bytes += int64(len(scope.Name)+len(scope.Kind)) * 2
	}
	for {
		memoryGeneration := shard.memoryGeneration.Load()
		shard.metadataMu.RLock()
		documentCST := shard.documentCST
		documentCSTGeneration := shard.documentCSTGeneration
		documentCSTEstimateFor := shard.documentCSTEstimateFor
		documentCSTEstimate := shard.documentCSTEstimate
		metadataReady := shard.metadataReady
		metadata := shard.metadata
		shard.metadataMu.RUnlock()

		if documentCST != nil && documentCSTEstimateFor != documentCST {
			documentCSTEstimate = documentCST.EstimateBytes()
			shard.metadataMu.Lock()
			if shard.documentCST == documentCST && shard.documentCSTGeneration == documentCSTGeneration {
				shard.documentCSTEstimateFor = documentCST
				shard.documentCSTEstimate = documentCSTEstimate
			}
			shard.metadataMu.Unlock()
		}

		if shard.memoryGeneration.Load() != memoryGeneration {
			continue
		}
		result := bytes
		if documentCST != nil {
			result += documentCSTEstimate
		}
		if metadataReady {
			result += estimateReferenceShardMetadataBytes(metadata)
		}
		return result
	}
}

// RuntimeAnalysisMemoryOwnerGeneration reports retained-size changes for the
// parsed-document runtime owner cache.
func (shard *ReferenceShard) RuntimeAnalysisMemoryOwnerGeneration() uint64 {
	if shard == nil {
		return 0
	}
	return shard.memoryGeneration.Load()
}

func (shard *ReferenceShard) documentCSTFor(parsed *core.ParsedDocument) *CSTNode {
	shard.metadataMu.RLock()
	root := shard.documentCST
	generation := shard.documentCSTGeneration
	shard.metadataMu.RUnlock()
	if root != nil {
		return root
	}
	built := parseDocumentCST(parsed)
	shard.metadataMu.Lock()
	if shard.documentCST == nil && shard.documentCSTGeneration == generation {
		shard.documentCST = built
		shard.documentCSTEstimateFor = nil
		shard.documentCSTEstimate = 0
		shard.memoryGeneration.Add(1)
	}
	root = shard.documentCST
	shard.metadataMu.Unlock()
	if root == nil {
		return built
	}
	return root
}

// ReleaseDocumentCST drops the transient document CST retained by the shard.
// A later ParseDocumentCST call safely rebuilds and reattaches the CST.
func (shard *ReferenceShard) ReleaseDocumentCST() {
	if shard == nil {
		return
	}
	shard.metadataMu.Lock()
	shard.documentCST = nil
	shard.documentCSTEstimateFor = nil
	shard.documentCSTEstimate = 0
	shard.documentCSTGeneration++
	shard.memoryGeneration.Add(1)
	shard.metadataMu.Unlock()
}

func estimateReferenceShardMetadataBytes(metadata referenceShardMetadata) int64 {
	bytes := int64(64 + cap(metadata.names)*16 + len(metadata.summaries)*256)
	for _, summary := range metadata.summaries {
		bytes += int64(len(summary.CountFingerprint)+len(summary.LocationFingerprint)) * 2
		bytes += int64(cap(summary.DeclarationRanges)) * 32
		bytes += int64(cap(summary.GlobalResolutions))
		bytes += int64(cap(summary.CallGroups)) * 96
		for _, group := range summary.CallGroups {
			bytes += int64(len(group.Scope)+len(group.ScopeKind)+len(group.Owner)+len(group.ClassOwner)) * 2
			bytes += int64(cap(group.PostingIndexes)) * 8
		}
	}
	return bytes
}

// DeclarationRangesFor returns the immutable declaration ranges for a case-insensitive name.
func (shard *ReferenceShard) DeclarationRangesFor(name string) []lsp.Range {
	summary, ok := shard.SummaryFor(name)
	if !ok {
		return nil
	}
	return summary.DeclarationRanges
}

// CallGroupsFor returns immutable indexes of call postings grouped by normalized scope and owner.
func (shard *ReferenceShard) CallGroupsFor(name string) []ReferenceCallGroup {
	summary, ok := shard.SummaryFor(name)
	if !ok {
		return nil
	}
	return summary.CallGroups
}

// GlobalResolutionsFor returns global-resolution decisions aligned with PostingsFor.
func (shard *ReferenceShard) GlobalResolutionsFor(name string) []bool {
	summary, ok := shard.SummaryFor(name)
	if !ok {
		return nil
	}
	return summary.GlobalResolutions
}

// BuildReferenceShard builds or restores the shared per-document VBScript reference index.
func BuildReferenceShard(parsed *core.ParsedDocument) *ReferenceShard {
	if parsed == nil {
		return &ReferenceShard{Version: referenceShardVersion, Declarations: map[string]Symbol{}, Postings: map[string][]ReferencePosting{}}
	}
	if !parsed.ChangeImpact.Affects(core.LanguageVBScript) {
		if cached, ok := parsed.LoadPreviousRuntimeAnalysis(referenceShardAnalysisKey); ok {
			if shard, valid := cached.(*ReferenceShard); valid && shard != nil && shard.Version == referenceShardVersion {
				if core.IncrementalChangeAfterLanguage(parsed, core.LanguageVBScript) {
					parsed.StoreRuntimeAnalysis(referenceShardAnalysisKey, shard)
					return shard
				}
				if shifted, ok := remapReferenceShard(parsed, shard); ok {
					parsed.StoreRuntimeAnalysis(referenceShardAnalysisKey, shifted)
					return shifted
				}
			}
		}
	}
	return buildReferenceShardSingleflight(parsed, buildReferenceShard)
}

func remapReferenceShard(parsed *core.ParsedDocument, previous *ReferenceShard) (*ReferenceShard, bool) {
	mapper, ok := core.IncrementalRangeMapperFor(parsed)
	if !ok || previous == nil {
		return nil, false
	}
	shifted := &ReferenceShard{
		Version:      previous.Version,
		Declarations: make(map[string]Symbol, len(previous.Declarations)),
		Postings:     make(map[string][]ReferencePosting, len(previous.Postings)),
		Scopes:       make([]ReferenceScope, len(previous.Scopes)),
	}
	for name, declaration := range previous.Declarations {
		declaration.Range, ok = mapper.Range(declaration.Range)
		if !ok {
			return nil, false
		}
		shifted.Declarations[name] = declaration
	}
	for name, postings := range previous.Postings {
		mapped := make([]ReferencePosting, len(postings))
		for index, posting := range postings {
			posting.Range, ok = mapper.Range(posting.Range)
			if !ok {
				return nil, false
			}
			mapped[index] = posting
		}
		shifted.Postings[name] = mapped
	}
	for index, scope := range previous.Scopes {
		scope.Range, ok = mapper.Range(scope.Range)
		if !ok {
			return nil, false
		}
		shifted.Scopes[index] = scope
	}
	return shifted, true
}

// CachedReferenceShard returns a validated in-memory or restored shard without
// building one from source.
func CachedReferenceShard(parsed *core.ParsedDocument) (*ReferenceShard, bool) {
	shard := cachedReferenceShard(parsed)
	return shard, shard != nil
}

func buildReferenceShardSingleflight(parsed *core.ParsedDocument, build func(*core.ParsedDocument) *ReferenceShard) *ReferenceShard {
	if shard := cachedReferenceShard(parsed); shard != nil {
		return shard
	}

	pending := &referenceShardBuild{done: make(chan struct{})}
	actual, loaded := referenceShardBuilds.LoadOrStore(parsed, pending)
	if loaded {
		pending = actual.(*referenceShardBuild)
		<-pending.done
		return pending.shard
	}
	defer func() {
		referenceShardBuilds.Delete(parsed)
		close(pending.done)
	}()

	if shard := cachedReferenceShard(parsed); shard != nil {
		pending.shard = shard
		return shard
	}
	shard := build(parsed)
	parsed.StoreRuntimeAnalysis(referenceShardAnalysisKey, shard)
	pending.shard = shard
	return shard
}

func cachedReferenceShard(parsed *core.ParsedDocument) *ReferenceShard {
	if cached, ok := parsed.LoadRuntimeAnalysis(referenceShardAnalysisKey); ok {
		if shard, valid := cached.(*ReferenceShard); valid && shard != nil && shard.Version == referenceShardVersion {
			return shard
		}
	}
	var restored ReferenceShard
	if parsed.LoadAnalysis(referenceShardAnalysisKey, &restored) && restored.Version == referenceShardVersion && restored.Declarations != nil && restored.Postings != nil {
		parsed.StoreRuntimeAnalysis(referenceShardAnalysisKey, &restored)
		return &restored
	}
	return nil
}

// SeedReferenceShard restores a validated reference shard into a parsed document's persistent and runtime caches.
func SeedReferenceShard(parsed *core.ParsedDocument, shard *ReferenceShard) {
	if parsed == nil || shard == nil || shard.Version != referenceShardVersion || shard.Declarations == nil || shard.Postings == nil {
		return
	}
	parsed.StoreAnalysis(referenceShardAnalysisKey, shard)
	parsed.StoreRuntimeAnalysis(referenceShardAnalysisKey, shard)
}

type referenceScopeOffsets struct {
	ReferenceScope
	start int
	end   int
}

type referenceScopeIndex struct {
	procedures []referenceScopeOffsets
	classes    []referenceScopeOffsets
}

type referenceShardRegion struct {
	region core.Region
	text   string
	spans  []identifierSpan
}

func (index *referenceScopeIndex) add(scopes []referenceScopeOffsets) {
	for _, scope := range scopes {
		if scope.Kind == "class" {
			index.classes = append(index.classes, scope)
		} else {
			index.procedures = append(index.procedures, scope)
		}
	}
}

func buildReferenceShard(parsed *core.ParsedDocument) *ReferenceShard {
	source := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	regions, structured := referenceShardRegions(parsed)
	var documentTokens []Token
	var documentCST *CSTNode
	if structured {
		documentCST = parseDocumentCST(parsed)
		// Tokenize each ASP island independently. The masked document CST cannot
		// supply this stream because an unterminated literal in one island must
		// not consume tokens from a later island.
		documentTokens = vbscriptDocumentTokens(parsed)
	}
	withOwners := newWithOwnerIndex(parsed.Text, documentTokens)
	shard := &ReferenceShard{
		Version:      referenceShardVersion,
		Declarations: map[string]Symbol{},
		Postings:     map[string][]ReferencePosting{},
	}
	shard.documentCST = documentCST
	var scopes referenceScopeIndex
	if structured {
		for _, scope := range referenceScopesFromTokens(source, parsed.Text, 0, documentTokens) {
			scopes.add([]referenceScopeOffsets{scope})
			shard.Scopes = append(shard.Scopes, scope.ReferenceScope)
		}
	}
	var cstDeclarations map[int]string
	if structured {
		cstDeclarations = cstDeclarationKinds(documentCST)
	}

	for _, item := range regions {
		region, text, spans := item.region, item.text, item.spans
		var flatDeclarations []string
		if !structured {
			flatDeclarations = flatReferenceDeclarationKinds(text, spans)
		}
		for index, span := range spans {
			start := region.ContentStart + span.Start
			end := region.ContentStart + span.End
			name := parsed.Text[start:end]
			lower := strings.ToLower(name)
			posting := ReferencePosting{Name: name, Range: source.Range(start, end)}
			if scope, ok := scopes.innermost(start); ok {
				posting.Scope = scope.Name
				posting.ScopeKind = scope.Kind
			}
			if classScope, ok := scopes.classAt(start); ok {
				posting.ClassOwner = classScope.Name
			}
			if structured {
				posting.Owner, _, _ = memberOwnerFromTokens(parsed.Text, start, withOwners)
			} else {
				posting.Owner = flatMemberOwner(parsed.Text, start)
			}

			declKind := ""
			declared := false
			if structured {
				declKind, declared = cstDeclarations[region.ContentStart+span.Start]
			}
			if !declared && len(flatDeclarations) > 0 && flatDeclarations[index] != "" {
				declKind = flatDeclarations[index]
				declared = true
			}
			if !declared {
				if fallbackKind, ok := declarationKindAt(text, spans, index); ok {
					declKind = fallbackKind
					declared = true
				}
			}
			if declared {
				posting.Roles |= ReferenceRoleDeclaration
				if _, exists := shard.Declarations[lower]; !exists {
					shard.Declarations[lower] = Symbol{Name: name, Kind: declKind, Range: posting.Range}
				}
			}

			if write, objectInitialization := assignmentRole(parsed.Text, start, end); write {
				posting.Roles |= ReferenceRoleWrite
				if objectInitialization {
					posting.Roles |= ReferenceRoleObjectInitialization
				}
			} else if !declared {
				posting.Roles |= ReferenceRoleRead
			}
			isProcedureDeclaration := declared && (declKind == "function" || declKind == "sub" || declKind == "property")
			if next := nextNonSpace(parsed.Text, end); next >= 0 && parsed.Text[next] == '(' && !isProcedureDeclaration {
				posting.Roles |= ReferenceRoleCall | ReferenceRoleRead
			}
			if posting.HasRole(ReferenceRoleWrite) && isFunctionReturnPosting(posting, lower) {
				posting.Roles |= ReferenceRoleFunctionReturn
			}
			shard.Postings[lower] = append(shard.Postings[lower], posting)
		}
		for _, occurrence := range xmlDocCrefOccurrences(source, text, region.ContentStart) {
			posting := ReferencePosting{
				Name:  occurrence.Name,
				Range: occurrence.Range,
				Roles: ReferenceRoleRead | ReferenceRoleCref,
			}
			offset := source.OffsetAt(occurrence.Range.Start)
			posting.Owner, _, _ = memberOwnerFromTokens(parsed.Text, offset, withOwners)
			if scope, ok := scopes.innermost(offset); ok {
				posting.Scope = scope.Name
				posting.ScopeKind = scope.Kind
			}
			if classScope, ok := scopes.classAt(offset); ok {
				posting.ClassOwner = classScope.Name
			}
			lower := strings.ToLower(posting.Name)
			shard.Postings[lower] = append(shard.Postings[lower], posting)
		}
	}
	for name := range shard.Postings {
		sort.SliceStable(shard.Postings[name], func(i, j int) bool {
			left := shard.Postings[name][i].Range.Start
			right := shard.Postings[name][j].Range.Start
			if left.Line != right.Line {
				return left.Line < right.Line
			}
			return left.Character < right.Character
		})
	}
	return shard
}

func referenceShardRegions(parsed *core.ParsedDocument) ([]referenceShardRegion, bool) {
	regions := make([]referenceShardRegion, 0, len(parsed.Regions))
	structured := false
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		text := parsed.Text[region.ContentStart:region.ContentEnd]
		spans := identifierSpans(text)
		for index := range spans {
			if referenceShardStructuredKeyword(text, spans, index) {
				structured = true
				break
			}
		}
		regions = append(regions, referenceShardRegion{region: region, text: text, spans: spans})
	}
	return regions, structured
}

func referenceShardStructuredKeyword(text string, spans []identifierSpan, index int) bool {
	if index < 0 || index >= len(spans) {
		return false
	}
	word := strings.ToLower(text[spans[index].Start:spans[index].End])
	if word != "with" && word != "sub" && word != "function" && word != "class" && word != "property" {
		return false
	}
	statementStart := index
	for statementStart > 0 && sameFlatReferenceStatement(text, spans[statementStart-1].End, spans[statementStart].Start) {
		statementStart--
	}
	if word == "with" {
		return statementStart == index
	}
	cursor := statementStart
	for cursor < len(spans) && (strings.EqualFold(text[spans[cursor].Start:spans[cursor].End], "public") || strings.EqualFold(text[spans[cursor].Start:spans[cursor].End], "private") || strings.EqualFold(text[spans[cursor].Start:spans[cursor].End], "default")) {
		cursor++
	}
	if cursor != index {
		return false
	}
	switch word {
	case "sub", "function", "class", "property":
		return true
	default:
		return false
	}
}

func flatReferenceDeclarationKinds(text string, spans []identifierSpan) []string {
	kinds := make([]string, len(spans))
	for index := range spans {
		if kind, ok := declarationKindAt(text, spans, index); ok {
			kinds[index] = kind
		}
	}
	for dimIndex, span := range spans {
		if !strings.EqualFold(text[span.Start:span.End], "dim") || !flatDimStatementStart(text, spans, dimIndex) {
			continue
		}
		expectName := true
		parenthesisDepth := 0
		previousEnd := span.End
		for index := dimIndex + 1; index < len(spans); index++ {
			if !sameFlatReferenceStatement(text, span.End, spans[index].Start) {
				break
			}
			scanFlatDimPunctuation(text[previousEnd:spans[index].Start], &parenthesisDepth, &expectName)
			if expectName && parenthesisDepth == 0 && !isVBKeyword(text[spans[index].Start:spans[index].End]) {
				kinds[index] = "variable"
				expectName = false
			}
			previousEnd = spans[index].End
		}
	}
	return kinds
}

func flatDimStatementStart(text string, spans []identifierSpan, index int) bool {
	if index == 0 {
		return true
	}
	return !sameFlatReferenceStatement(text, spans[index-1].End, spans[index].Start)
}

func sameFlatReferenceStatement(text string, start, end int) bool {
	if start > end {
		start, end = end, start
	}
	for index := start; index < end; index++ {
		switch text[index] {
		case '"':
			index++
			for index < end {
				if text[index] != '"' {
					index++
					continue
				}
				index++
				if index < end && text[index] == '"' {
					index++
					continue
				}
				break
			}
		case ':':
			return false
		case '\r', '\n':
			if text[index] == '\n' && index > start && text[index-1] == '\r' {
				continue
			}
			if !flatLineContinues(text, index) {
				return false
			}
		}
	}
	return true
}

func flatLineContinues(text string, newline int) bool {
	if flatPhysicalLineHasComment(text, newline) {
		return false
	}
	for index := newline - 1; index >= 0 && (text[index] == ' ' || text[index] == '\t'); index-- {
		newline = index
	}
	if newline <= 0 || text[newline-1] != '_' {
		return false
	}
	return newline < 2 || !isIdent(text[newline-2])
}

func flatPhysicalLineHasComment(text string, end int) bool {
	start := end
	for start > 0 && text[start-1] != '\r' && text[start-1] != '\n' {
		start--
	}
	statementStart := true
	for index := start; index < end; {
		switch text[index] {
		case ' ', '\t':
			index++
		case '\'':
			return true
		case ':':
			statementStart = true
			index++
		case '"':
			statementStart = false
			index++
			for index < end {
				if text[index] != '"' {
					index++
					continue
				}
				index++
				if index < end && text[index] == '"' {
					index++
					continue
				}
				break
			}
		default:
			wordEnd := index
			for wordEnd < end && isIdent(text[wordEnd]) {
				wordEnd++
			}
			if statementStart && wordEnd > index && strings.EqualFold(text[index:wordEnd], "rem") {
				return true
			}
			statementStart = false
			if wordEnd == index {
				index++
			} else {
				index = wordEnd
			}
		}
	}
	return false
}

func scanFlatDimPunctuation(text string, depth *int, expectName *bool) {
	for index := 0; index < len(text); index++ {
		switch text[index] {
		case '"':
			index++
			for index < len(text) {
				if text[index] != '"' {
					index++
					continue
				}
				index++
				if index < len(text) && text[index] == '"' {
					index++
					continue
				}
				break
			}
		case '\'':
			return
		case '(':
			(*depth)++
		case ')':
			if *depth > 0 {
				(*depth)--
			}
		case ',':
			if *depth == 0 {
				*expectName = true
			}
		}
	}
}

func flatMemberOwner(text string, start int) string {
	cursor := start
	for cursor > 0 && (text[cursor-1] == ' ' || text[cursor-1] == '\t') {
		cursor--
	}
	if cursor == 0 || text[cursor-1] != '.' {
		return ""
	}
	ownerStart, ownerEnd, ok := dottedOwnerBounds(text, cursor-1)
	if !ok || hasLeadingDotBefore(text, ownerStart) {
		return ""
	}
	return strings.ToLower(text[ownerStart:ownerEnd])
}

func sortedReferenceShardNames(shard *ReferenceShard) []string {
	names := make(map[string]struct{}, len(shard.Postings)+len(shard.Declarations))
	for name := range shard.Postings {
		names[strings.ToLower(name)] = struct{}{}
	}
	for name := range shard.Declarations {
		names[strings.ToLower(name)] = struct{}{}
	}
	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func summarizeReferenceName(name string, postings []ReferencePosting, declaration Symbol, declared bool) ReferenceNameSummary {
	summary := ReferenceNameSummary{
		Postings:          len(postings),
		Declared:          declared,
		GlobalResolutions: GlobalReferenceResolutions(postings),
	}
	type callGroupKey struct {
		scope      string
		scopeKind  string
		owner      string
		classOwner string
	}
	callGroups := map[callGroupKey][]int{}
	for index, posting := range postings {
		if posting.HasRole(ReferenceRoleDeclaration) {
			summary.Roles.Declarations++
			summary.DeclarationRanges = append(summary.DeclarationRanges, posting.Range)
		}
		if posting.HasRole(ReferenceRoleRead) {
			summary.Roles.Reads++
		}
		if posting.HasRole(ReferenceRoleWrite) {
			summary.Roles.Writes++
		}
		if posting.HasRole(ReferenceRoleCall) {
			summary.Roles.Calls++
			key := callGroupKey{
				scope:      strings.ToLower(posting.Scope),
				scopeKind:  strings.ToLower(posting.ScopeKind),
				owner:      strings.ToLower(posting.Owner),
				classOwner: strings.ToLower(posting.ClassOwner),
			}
			callGroups[key] = append(callGroups[key], index)
		}
		if posting.HasRole(ReferenceRoleFunctionReturn) {
			summary.Roles.FunctionReturns++
		}
		if posting.HasRole(ReferenceRoleObjectInitialization) {
			summary.Roles.ObjectInitializations++
		}
		if posting.HasRole(ReferenceRoleCref) {
			summary.Roles.Crefs++
		}
	}
	keys := make([]callGroupKey, 0, len(callGroups))
	for key := range callGroups {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].scope != keys[j].scope {
			return keys[i].scope < keys[j].scope
		}
		if keys[i].scopeKind != keys[j].scopeKind {
			return keys[i].scopeKind < keys[j].scopeKind
		}
		if keys[i].classOwner != keys[j].classOwner {
			return keys[i].classOwner < keys[j].classOwner
		}
		return keys[i].owner < keys[j].owner
	})
	for _, key := range keys {
		summary.CallGroups = append(summary.CallGroups, ReferenceCallGroup{
			Scope: key.scope, ScopeKind: key.scopeKind, Owner: key.owner, ClassOwner: key.classOwner, PostingIndexes: callGroups[key],
		})
	}
	summary.CountFingerprint = referenceCountFingerprint(summary)
	summary.LocationFingerprint = referenceLocationFingerprint(name, postings, declaration, declared)
	return summary
}

func referenceCountFingerprint(summary ReferenceNameSummary) string {
	hash := sha256.New()
	var number [8]byte
	values := [...]int{
		summary.Postings,
		summary.Roles.Declarations,
		summary.Roles.Reads,
		summary.Roles.Writes,
		summary.Roles.Calls,
		summary.Roles.FunctionReturns,
		summary.Roles.ObjectInitializations,
		summary.Roles.Crefs,
	}
	for _, value := range values {
		binary.LittleEndian.PutUint64(number[:], uint64(value))
		_, _ = hash.Write(number[:])
	}
	if summary.Declared {
		_, _ = hash.Write([]byte{1})
	} else {
		_, _ = hash.Write([]byte{0})
	}
	for _, resolved := range summary.GlobalResolutions {
		if resolved {
			_, _ = hash.Write([]byte{1})
		} else {
			_, _ = hash.Write([]byte{0})
		}
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func referenceLocationFingerprint(name string, postings []ReferencePosting, declaration Symbol, declared bool) string {
	hash := sha256.New()
	var number [8]byte
	writeString := func(value string) {
		binary.LittleEndian.PutUint64(number[:], uint64(len(value)))
		_, _ = hash.Write(number[:])
		_, _ = hash.Write([]byte(value))
	}
	writePosition := func(position lsp.Position) {
		binary.LittleEndian.PutUint32(number[:4], uint32(position.Line))
		binary.LittleEndian.PutUint32(number[4:], uint32(position.Character))
		_, _ = hash.Write(number[:])
	}
	writeString(name)
	if declared {
		_, _ = hash.Write([]byte{1})
		writeString(declaration.Name)
		writeString(declaration.Kind)
		writePosition(declaration.Range.Start)
		writePosition(declaration.Range.End)
	} else {
		_, _ = hash.Write([]byte{0})
	}
	for _, posting := range postings {
		writeString(posting.Name)
		writePosition(posting.Range.Start)
		writePosition(posting.Range.End)
		binary.LittleEndian.PutUint64(number[:], uint64(posting.Roles))
		_, _ = hash.Write(number[:])
		writeString(posting.Scope)
		writeString(posting.ScopeKind)
		writeString(posting.Owner)
		writeString(posting.ClassOwner)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func referenceScopesFromTokens(source *core.TextDocument, text string, base int, tokens []Token) []referenceScopeOffsets {
	type openScope struct {
		name  string
		kind  string
		start int
	}
	var open []openScope
	var result []referenceScopeOffsets
	for index := 0; index < len(tokens); index++ {
		if !isCSTStatementStart(tokens, index) {
			continue
		}
		first := strings.ToLower(tokens[index].Text)
		if first == "end" && index+1 < len(tokens) {
			kind := strings.ToLower(tokens[index+1].Text)
			if kind == "sub" || kind == "function" || kind == "property" || kind == "class" {
				for cursor := len(open) - 1; cursor >= 0; cursor-- {
					if open[cursor].kind != kind && !(kind == "property" && strings.HasPrefix(open[cursor].kind, "property-")) {
						continue
					}
					end := cstStatementEnd(tokens, index)
					item := open[cursor]
					result = append(result, referenceScopeOffsets{
						ReferenceScope: ReferenceScope{Name: item.name, Kind: item.kind, Range: source.Range(item.start, end)},
						start:          item.start,
						end:            end,
					})
					open = open[:cursor]
					break
				}
			}
			continue
		}
		cursor := index
		for first == "public" || first == "private" || first == "default" {
			cursor++
			if cursor >= len(tokens) {
				break
			}
			first = strings.ToLower(tokens[cursor].Text)
		}
		if cursor >= len(tokens) {
			continue
		}
		kind := first
		nameIndex := cursor + 1
		if first == "property" {
			if nameIndex >= len(tokens) {
				continue
			}
			accessor := strings.ToLower(tokens[nameIndex].Text)
			if accessor != "get" && accessor != "let" && accessor != "set" {
				continue
			}
			kind += "-" + accessor
			nameIndex++
		}
		if kind != "sub" && kind != "function" && kind != "class" && !strings.HasPrefix(kind, "property-") {
			continue
		}
		if nameIndex >= len(tokens) || tokens[nameIndex].Kind != "identifier" {
			continue
		}
		open = append(open, openScope{
			name:  tokens[nameIndex].Text,
			kind:  kind,
			start: tokens[cursor].Start,
		})
	}
	for _, item := range open {
		end := base + len(text)
		result = append(result, referenceScopeOffsets{
			ReferenceScope: ReferenceScope{Name: item.name, Kind: item.kind, Range: source.Range(item.start, end)},
			start:          item.start,
			end:            end,
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].start != result[j].start {
			return result[i].start < result[j].start
		}
		return result[i].end > result[j].end
	})
	return result
}

func (index referenceScopeIndex) innermost(offset int) (ReferenceScope, bool) {
	if scope, ok := containingReferenceScope(index.procedures, offset); ok {
		return scope, true
	}
	return containingReferenceScope(index.classes, offset)
}

func (index referenceScopeIndex) classAt(offset int) (ReferenceScope, bool) {
	return containingReferenceScope(index.classes, offset)
}

func containingReferenceScope(scopes []referenceScopeOffsets, offset int) (ReferenceScope, bool) {
	cursor := sort.Search(len(scopes), func(index int) bool { return scopes[index].start > offset }) - 1
	if cursor < 0 || offset > scopes[cursor].end {
		return ReferenceScope{}, false
	}
	return scopes[cursor].ReferenceScope, true
}

func assignmentRole(text string, start int, end int) (bool, bool) {
	statementStart, statementEnd := statementBounds(text, start)
	prefix := strings.TrimSpace(text[statementStart:start])
	lowerPrefix := strings.ToLower(prefix)
	for _, separator := range []string{" then ", " else "} {
		if split := strings.LastIndex(lowerPrefix, separator); split >= 0 {
			prefix = strings.TrimSpace(prefix[split+len(separator):])
			lowerPrefix = strings.ToLower(prefix)
		}
	}
	if len(prefix) >= len("set") && strings.EqualFold(prefix[:len("set")], "set") && (len(prefix) == len("set") || prefix[len("set")] == ' ' || prefix[len("set")] == '\t') {
		prefix = strings.TrimSpace(prefix[len("set"):])
	}
	if prefix != "" && !strings.HasSuffix(prefix, ".") {
		return false, false
	}
	cursor := end
	for cursor < statementEnd && (text[cursor] == ' ' || text[cursor] == '\t') {
		cursor++
	}
	if cursor >= statementEnd || text[cursor] != '=' {
		return false, false
	}
	rhs := strings.ToLower(strings.TrimSpace(text[cursor+1 : statementEnd]))
	return true, isObjectInitializerExpression(rhs)
}

func isObjectInitializerExpression(rhs string) bool {
	if strings.HasPrefix(rhs, "new ") {
		return true
	}
	if !strings.Contains(rhs, "createobject") && !strings.Contains(rhs, "getobject") {
		return false
	}
	var compact strings.Builder
	compact.Grow(len(rhs))
	for _, value := range rhs {
		if value != ' ' && value != '\t' {
			compact.WriteRune(value)
		}
	}
	value := compact.String()
	return strings.HasPrefix(value, "createobject(") || strings.Contains(value, ".createobject(") || strings.HasPrefix(value, "getobject(") || strings.Contains(value, ".getobject(")
}

func statementBounds(text string, offset int) (int, int) {
	start := offset
	for start > 0 && text[start-1] != '\n' && text[start-1] != '\r' && text[start-1] != ':' {
		start--
	}
	end := offset
	for end < len(text) && text[end] != '\n' && text[end] != '\r' && text[end] != ':' {
		end++
	}
	return start, end
}

func isFunctionReturnPosting(posting ReferencePosting, normalizedName string) bool {
	return posting.ScopeKind == "function" && strings.EqualFold(posting.Scope, normalizedName)
}

func memberOwner(text string, start int) string {
	cursor := start
	for cursor > 0 && (text[cursor-1] == ' ' || text[cursor-1] == '\t') {
		cursor--
	}
	if cursor == 0 || text[cursor-1] != '.' {
		return ""
	}
	cursor--
	for cursor > 0 && (text[cursor-1] == ' ' || text[cursor-1] == '\t') {
		cursor--
	}
	end := cursor
	for cursor > 0 && isIdent(text[cursor-1]) {
		cursor--
	}
	if cursor == end {
		return ""
	}
	return strings.ToLower(text[cursor:end])
}
