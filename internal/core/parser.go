package core

import (
	"encoding/json"
	"math"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

type EmbeddedLanguage string

const (
	LanguageHTML         EmbeddedLanguage = "html"
	LanguageCSS          EmbeddedLanguage = "css"
	LanguageJavaScript   EmbeddedLanguage = "javascript"
	LanguageVBScript     EmbeddedLanguage = "vbscript"
	LanguageJScript      EmbeddedLanguage = "jscript"
	LanguageASPDirective EmbeddedLanguage = "asp-directive"
)

type RegionKind string

const (
	RegionHTML           RegionKind = "html"
	RegionASPBlock       RegionKind = "asp-block"
	RegionASPExpression  RegionKind = "asp-expression"
	RegionASPDirective   RegionKind = "asp-directive"
	RegionStyle          RegionKind = "style"
	RegionStyleAttribute RegionKind = "style-attribute"
	RegionClientScript   RegionKind = "client-script"
	RegionServerScript   RegionKind = "server-script"
)

type Region struct {
	Kind         RegionKind
	Language     EmbeddedLanguage
	Start        int
	End          int
	ContentStart int
	ContentEnd   int
}

type Include struct {
	Path  string
	Mode  string
	Range lsp.Range
}

type ParseError struct {
	Start   int
	End     int
	Message string
}

type ParsedDocument struct {
	URI             string
	Text            string
	DefaultLanguage EmbeddedLanguage
	Regions         []Region
	Includes        []Include
	Errors          []ParseError
	// ChangeImpact is a runtime-only query input and is never persisted in the disk cache.
	ChangeImpact IncrementalImpact `json:"-"`
	// Analysis stores versioned, source-derived facts that can be restored with the parsed document.
	Analysis map[string]json.RawMessage `json:"analysis,omitempty"`
	// runtimeAnalysis keeps immutable decoded facts for the lifetime of this parsed source.
	runtimeAnalysis           map[string]any
	previousRevisionText      string
	previousRuntimeAnalysis   map[string]any
	hasPreviousRevision       bool
	runtimeAnalysisOwnerCache *runtimeAnalysisMemoryOwnerCache
	// analysisMu holds the *sync.RWMutex returned by analysisLock.
	analysisMu unsafe.Pointer
}

type runtimeAnalysisEntry struct {
	value any
}

type runtimeAnalysisMemoryOwnerCache struct {
	generation          uint64
	owners              []RuntimeAnalysisMemoryOwner
	ownersGeneration    uint64
	providerGenerations []runtimeAnalysisOwnerGeneration
	valid               bool
}

// RuntimeAnalysisMemoryOwner identifies one immutable runtime value or backing
// component that may be shared by adjacent parsed revisions. Identity is
// comparable and remains stable for the lifetime of the retained value.
type RuntimeAnalysisMemoryOwner struct {
	Identity any
	Bytes    int64
}

// RuntimeAnalysisMemoryOwnerProvider exposes component-level ownership for a
// runtime value whose storage can be shared across parsed revisions. Returned
// identities must be comparable and non-nil; byte estimates must be positive.
type RuntimeAnalysisMemoryOwnerProvider interface {
	// RuntimeAnalysisMemoryOwnerSet returns independently shareable runtime
	// components and their approximate retained sizes.
	RuntimeAnalysisMemoryOwnerSet() []RuntimeAnalysisMemoryOwner
}

// RuntimeAnalysisMemoryOwnerGenerationProvider reports a monotonically
// increasing generation for mutable runtime storage. Implementations must
// update the generation whenever RuntimeAnalysisMemoryOwnerSet or
// EstimateBytes would observe a different retained-size view. The method
// must be side-effect free and safe to call on the stable owner-cache path.
type RuntimeAnalysisMemoryOwnerGenerationProvider interface {
	RuntimeAnalysisMemoryOwnerGeneration() uint64
}

// RuntimeAnalysisExclusiveEstimator reports immutable runtime storage that is
// not represented by a value's generic fields. A top-level EstimateBytes
// method remains a complete estimate override; this interface is additive and
// is used after granular backing traversal.
type RuntimeAnalysisExclusiveEstimator interface {
	EstimateExclusiveRuntimeBytes() int64
}

// analysisLock returns the lock guarding this revision's analysis maps. It is
// allocated on first use; value copies made afterwards share it, together with
// the maps they share, with the original.
func (p *ParsedDocument) analysisLock() *sync.RWMutex {
	if mu := (*sync.RWMutex)(atomic.LoadPointer(&p.analysisMu)); mu != nil {
		return mu
	}
	mu := new(sync.RWMutex)
	if atomic.CompareAndSwapPointer(&p.analysisMu, nil, unsafe.Pointer(mu)) {
		return mu
	}
	return (*sync.RWMutex)(atomic.LoadPointer(&p.analysisMu))
}

// LoadAnalysis decodes a cached source-derived analysis value.
func (p *ParsedDocument) LoadAnalysis(key string, target any) bool {
	if p == nil || target == nil {
		return false
	}
	p.analysisLock().RLock()
	payload := append(json.RawMessage(nil), p.Analysis[key]...)
	p.analysisLock().RUnlock()
	return len(payload) > 0 && json.Unmarshal(payload, target) == nil
}

// StoreAnalysis encodes a source-derived analysis value for persistence with the document.
func (p *ParsedDocument) StoreAnalysis(key string, value any) {
	if p == nil || value == nil {
		return
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return
	}
	p.analysisLock().Lock()
	if p.Analysis == nil {
		p.Analysis = map[string]json.RawMessage{}
	}
	p.Analysis[key] = payload
	p.analysisLock().Unlock()
}

// LoadRuntimeAnalysis returns an immutable decoded fact cached for this parsed source.
func (p *ParsedDocument) LoadRuntimeAnalysis(key string) (any, bool) {
	if p == nil {
		return nil, false
	}
	p.analysisLock().RLock()
	entry, ok := p.runtimeAnalysis[key].(*runtimeAnalysisEntry)
	p.analysisLock().RUnlock()
	if !ok || entry == nil {
		return nil, false
	}
	return entry.value, true
}

// StoreRuntimeAnalysis caches an immutable decoded fact for this parsed source.
func (p *ParsedDocument) StoreRuntimeAnalysis(key string, value any) {
	if p == nil || value == nil {
		return
	}
	p.analysisLock().Lock()
	if p.runtimeAnalysis == nil {
		p.runtimeAnalysis = map[string]any{}
	}
	entry := &runtimeAnalysisEntry{value: value}
	if previous, ok := p.previousRuntimeAnalysis[key].(*runtimeAnalysisEntry); ok && previous != nil && sameRuntimeAnalysisBacking(previous.value, value) {
		entry = previous
	}
	p.runtimeAnalysis[key] = entry
	delete(p.previousRuntimeAnalysis, key)
	p.invalidateRuntimeAnalysisMemoryOwnersLocked()
	p.analysisLock().Unlock()
}

func (p *ParsedDocument) invalidateRuntimeAnalysisMemoryOwnersLocked() {
	cache := p.runtimeAnalysisOwnerCache
	if cache == nil {
		return
	}
	cache.generation++
	cache.owners = nil
	cache.ownersGeneration = 0
	cache.providerGenerations = nil
	cache.valid = false
}

func sameRuntimeAnalysisBacking(first, second any) bool {
	if first == nil || second == nil || reflect.TypeOf(first) != reflect.TypeOf(second) {
		return false
	}
	firstValue := reflect.ValueOf(first)
	secondValue := reflect.ValueOf(second)
	switch firstValue.Kind() {
	case reflect.Chan, reflect.Map, reflect.Ptr, reflect.UnsafePointer:
		return firstValue.UnsafePointer() == secondValue.UnsafePointer()
	case reflect.Slice:
		return firstValue.Len() == secondValue.Len() && firstValue.Cap() == secondValue.Cap() && firstValue.UnsafePointer() == secondValue.UnsafePointer()
	case reflect.String:
		return firstValue.Len() == secondValue.Len() && firstValue.UnsafePointer() == secondValue.UnsafePointer()
	default:
		return false
	}
}

func sameRuntimeAnalysisValue(first, second any) bool {
	if first == nil || second == nil || reflect.TypeOf(first) != reflect.TypeOf(second) {
		return false
	}
	firstValue := reflect.ValueOf(first)
	secondValue := reflect.ValueOf(second)
	if firstValue.Comparable() && secondValue.Comparable() {
		return firstValue.Interface() == secondValue.Interface()
	}
	return sameRuntimeAnalysisBacking(first, second)
}

// LoadOrStoreRuntimeAnalysis atomically installs an immutable runtime fact for
// this parsed source. An inherited predecessor remains available until the
// caller finishes any reuse or initialization decision and calls
// ReleasePreviousRuntimeAnalysis or StoreRuntimeAnalysis.
func (p *ParsedDocument) LoadOrStoreRuntimeAnalysis(key string, value any) (actual any, loaded bool) {
	if p == nil || value == nil {
		return nil, false
	}
	p.analysisLock().Lock()
	defer p.analysisLock().Unlock()
	if entry, ok := p.runtimeAnalysis[key].(*runtimeAnalysisEntry); ok && entry != nil {
		return entry.value, true
	}
	if p.runtimeAnalysis == nil {
		p.runtimeAnalysis = map[string]any{}
	}
	entry := &runtimeAnalysisEntry{value: value}
	if previous, ok := p.previousRuntimeAnalysis[key].(*runtimeAnalysisEntry); ok && previous != nil && sameRuntimeAnalysisBacking(previous.value, value) {
		entry = previous
	}
	p.runtimeAnalysis[key] = entry
	p.invalidateRuntimeAnalysisMemoryOwnersLocked()
	return entry.value, false
}

// ReleasePreviousRuntimeAnalysis drops an inherited fact after the current
// value has finished initialization. The expected value guard keeps a stale
// initializer from releasing a predecessor after another value has replaced
// the current entry.
func (p *ParsedDocument) ReleasePreviousRuntimeAnalysis(key string, expected any) bool {
	if p == nil || expected == nil {
		return false
	}
	p.analysisLock().Lock()
	defer p.analysisLock().Unlock()
	entry, ok := p.runtimeAnalysis[key].(*runtimeAnalysisEntry)
	if !ok || entry == nil || !sameRuntimeAnalysisValue(entry.value, expected) {
		return false
	}
	if _, ok := p.previousRuntimeAnalysis[key]; !ok {
		return false
	}
	delete(p.previousRuntimeAnalysis, key)
	p.invalidateRuntimeAnalysisMemoryOwnersLocked()
	return true
}

// PreviousRevisionText returns the immediate source predecessor retained for
// incremental range remapping. Older revisions are never linked or retained.
func (p *ParsedDocument) PreviousRevisionText() (string, bool) {
	if p == nil || !p.hasPreviousRevision {
		return "", false
	}
	return p.previousRevisionText, true
}

// LoadPreviousRuntimeAnalysis returns a runtime fact inherited from the
// immediate predecessor without retaining the predecessor document.
func (p *ParsedDocument) LoadPreviousRuntimeAnalysis(key string) (any, bool) {
	if p == nil {
		return nil, false
	}
	p.analysisLock().RLock()
	entry, ok := p.previousRuntimeAnalysis[key].(*runtimeAnalysisEntry)
	p.analysisLock().RUnlock()
	if !ok || entry == nil {
		return nil, false
	}
	return entry.value, true
}

func (p *ParsedDocument) inheritPreviousRevision(previous *ParsedDocument) {
	if p == nil || previous == nil {
		return
	}
	var inherited map[string]any
	previousLock := previous.analysisLock()
	previousLock.RLock()
	if len(previous.runtimeAnalysis) > 0 {
		inherited = make(map[string]any, len(previous.runtimeAnalysis))
		for key, value := range previous.runtimeAnalysis {
			entry, ok := value.(*runtimeAnalysisEntry)
			if !ok || entry == nil {
				continue
			}
			if _, skip := entry.value.(interface{ SkipPreviousRuntimeInheritance() }); skip {
				continue
			}
			inherited[key] = entry
		}
	}
	previousLock.RUnlock()
	p.analysisLock().Lock()
	p.previousRevisionText = previous.Text
	p.hasPreviousRevision = true
	if !sameRuntimeAnalysisMappings(p.previousRuntimeAnalysis, inherited) {
		p.previousRuntimeAnalysis = inherited
		p.invalidateRuntimeAnalysisMemoryOwnersLocked()
	}
	p.analysisLock().Unlock()
}

func sameRuntimeAnalysisMappings(first, second map[string]any) bool {
	if len(first) != len(second) {
		return false
	}
	for key, firstValue := range first {
		secondValue, ok := second[key]
		if !ok {
			return false
		}
		firstEntry, firstOK := firstValue.(*runtimeAnalysisEntry)
		secondEntry, secondOK := secondValue.(*runtimeAnalysisEntry)
		if !firstOK || !secondOK || firstEntry != secondEntry {
			return false
		}
	}
	return true
}

// AnalysisSnapshot returns an isolated copy suitable for concurrent persistence.
func (p *ParsedDocument) AnalysisSnapshot() map[string]json.RawMessage {
	if p == nil {
		return nil
	}
	p.analysisLock().RLock()
	snapshot := make(map[string]json.RawMessage, len(p.Analysis))
	for key, payload := range p.Analysis {
		snapshot[key] = append(json.RawMessage(nil), payload...)
	}
	p.analysisLock().RUnlock()
	return snapshot
}

// CloneStructural returns an independent copy of the parsed source structure
// and persisted analysis. Runtime analysis and incremental predecessor state
// are intentionally omitted so the clone does not retain backing storage from
// another parsed revision.
func (p *ParsedDocument) CloneStructural() *ParsedDocument {
	if p == nil {
		return nil
	}

	clone := &ParsedDocument{
		URI:             strings.Clone(p.URI),
		Text:            strings.Clone(p.Text),
		DefaultLanguage: EmbeddedLanguage(strings.Clone(string(p.DefaultLanguage))),
	}
	if len(p.Regions) > 0 {
		clone.Regions = make([]Region, len(p.Regions))
		for index, region := range p.Regions {
			clone.Regions[index] = Region{
				Kind:         RegionKind(strings.Clone(string(region.Kind))),
				Language:     EmbeddedLanguage(strings.Clone(string(region.Language))),
				Start:        region.Start,
				End:          region.End,
				ContentStart: region.ContentStart,
				ContentEnd:   region.ContentEnd,
			}
		}
	}
	if len(p.Includes) > 0 {
		clone.Includes = make([]Include, len(p.Includes))
		for index, include := range p.Includes {
			clone.Includes[index] = Include{
				Path:  strings.Clone(include.Path),
				Mode:  strings.Clone(include.Mode),
				Range: include.Range,
			}
		}
	}
	if len(p.Errors) > 0 {
		clone.Errors = make([]ParseError, len(p.Errors))
		for index, parseError := range p.Errors {
			clone.Errors[index] = ParseError{
				Start:   parseError.Start,
				End:     parseError.End,
				Message: strings.Clone(parseError.Message),
			}
		}
	}

	p.analysisLock().RLock()
	if p.Analysis != nil {
		clone.Analysis = make(map[string]json.RawMessage, len(p.Analysis))
		for key, payload := range p.Analysis {
			clone.Analysis[strings.Clone(key)] = append(json.RawMessage(nil), payload...)
		}
	}
	p.analysisLock().RUnlock()
	return clone
}

// EstimateBytes reports an approximate size of the source and parser state
// retained by this parsed revision. It does not decode persisted Analysis.
// Runtime values are measured from their retained backing storage; adjacent
// revisions that share one immutable value are charged only once.
func (p *ParsedDocument) EstimateBytes() int64 {
	if p == nil {
		return 0
	}

	snapshot := p.memoryAccountingSnapshot()
	owners := make(map[any]struct{}, len(snapshot.structuralOwners))
	bytes := snapshot.structuralBytes
	for identity, ownerBytes := range snapshot.structuralOwners {
		if _, exists := owners[identity]; exists {
			continue
		}
		owners[identity] = struct{}{}
		bytes = saturatingAddBytes(bytes, ownerBytes)
	}
	runtimeOwners := make(map[any]int64, len(snapshot.runtimeEntries))
	collectRuntimeAnalysisMemoryOwners(runtimeOwners, snapshot.runtimeEntries)
	for identity, ownerBytes := range runtimeOwners {
		if _, exists := owners[identity]; exists {
			continue
		}
		owners[identity] = struct{}{}
		bytes = saturatingAddBytes(bytes, ownerBytes)
	}
	return bytes
}

// EstimateStructuralBytes reports storage unique to this parsed revision and
// excludes runtime values that may be shared with an adjacent revision.
func (p *ParsedDocument) EstimateStructuralBytes() int64 {
	if p == nil {
		return 0
	}
	p.analysisLock().RLock()
	defer p.analysisLock().RUnlock()
	return p.estimateStructuralBytesLocked()
}

// RuntimeAnalysisMemoryOwners returns the shared runtime values retained by
// this revision without materializing any lazy analysis. The returned slice
// and its elements are immutable and may be shared by subsequent calls; callers
// must not modify them.
func (p *ParsedDocument) RuntimeAnalysisMemoryOwners() []RuntimeAnalysisMemoryOwner {
	if p == nil {
		return nil
	}

	for {
		p.analysisLock().RLock()
		cache := p.runtimeAnalysisOwnerCache
		if cache != nil && cache.valid && cache.ownersGeneration == cache.generation && runtimeAnalysisOwnerGenerationsMatch(cache.providerGenerations) {
			result := cache.owners
			p.analysisLock().RUnlock()
			return result
		}
		p.analysisLock().RUnlock()

		p.analysisLock().Lock()
		cache = p.runtimeAnalysisOwnerCache
		if cache == nil {
			cache = &runtimeAnalysisMemoryOwnerCache{}
			p.runtimeAnalysisOwnerCache = cache
		}
		if cache.valid && cache.ownersGeneration == cache.generation && runtimeAnalysisOwnerGenerationsMatch(cache.providerGenerations) {
			result := cache.owners
			p.analysisLock().Unlock()
			return result
		}
		generation := cache.generation
		entries := make([]runtimeAnalysisEntrySnapshot, 0, len(p.runtimeAnalysis)+len(p.previousRuntimeAnalysis))
		appendRuntimeAnalysisEntrySnapshots(&entries, p.runtimeAnalysis)
		appendRuntimeAnalysisEntrySnapshots(&entries, p.previousRuntimeAnalysis)
		p.analysisLock().Unlock()

		owners := make(map[any]int64, len(entries))
		providerGenerations := collectRuntimeAnalysisMemoryOwners(owners, entries)
		result := make([]RuntimeAnalysisMemoryOwner, 0, len(owners))
		for identity, bytes := range owners {
			result = append(result, RuntimeAnalysisMemoryOwner{Identity: exportedMemoryOwnerIdentity(identity), Bytes: bytes})
		}

		p.analysisLock().Lock()
		cache = p.runtimeAnalysisOwnerCache
		if cache == nil || cache.generation != generation || !runtimeAnalysisOwnerGenerationsMatch(providerGenerations) {
			p.analysisLock().Unlock()
			continue
		}
		if cache.valid && cache.ownersGeneration == generation && runtimeAnalysisOwnerGenerationsMatch(cache.providerGenerations) {
			result = cache.owners
		} else {
			cache.owners = result
			cache.ownersGeneration = generation
			cache.providerGenerations = providerGenerations
			cache.valid = true
		}
		p.analysisLock().Unlock()
		return result
	}
}

// EstimateStructuralBytesWithoutRevisionText reports structural storage other
// than the current source text and the immediate predecessor text retained for
// incremental remapping. Use StructuralMemoryOwners to charge those shared
// immutable strings once across adjacent revisions.
func (p *ParsedDocument) EstimateStructuralBytesWithoutRevisionText() int64 {
	if p == nil {
		return 0
	}
	p.analysisLock().RLock()
	defer p.analysisLock().RUnlock()
	return p.estimateStructuralBytesWithoutRevisionTextLocked()
}

// StructuralMemoryOwners returns the source strings retained by this parsed
// revision that can be shared with another parsed revision. The current text
// and predecessor text use the same backing identity when an incremental
// update retains an unchanged string.
func (p *ParsedDocument) StructuralMemoryOwners() []RuntimeAnalysisMemoryOwner {
	if p == nil {
		return nil
	}
	p.analysisLock().RLock()
	defer p.analysisLock().RUnlock()
	owners := p.structuralMemoryOwnersLocked()
	result := make([]RuntimeAnalysisMemoryOwner, 0, len(owners))
	for identity, bytes := range owners {
		result = append(result, RuntimeAnalysisMemoryOwner{Identity: exportedMemoryOwnerIdentity(identity), Bytes: bytes})
	}
	return result
}

type runtimeAnalysisEntrySnapshot struct {
	value        any
	rootIdentity any
}

type runtimeAnalysisOwnerGeneration struct {
	provider   RuntimeAnalysisMemoryOwnerGenerationProvider
	generation uint64
}

type parsedDocumentMemorySnapshot struct {
	structuralBytes  int64
	structuralOwners map[any]int64
	runtimeEntries   []runtimeAnalysisEntrySnapshot
}

func (p *ParsedDocument) memoryAccountingSnapshot() parsedDocumentMemorySnapshot {
	p.analysisLock().RLock()
	defer p.analysisLock().RUnlock()

	snapshot := parsedDocumentMemorySnapshot{
		structuralBytes:  p.estimateStructuralBytesWithoutRevisionTextLocked(),
		structuralOwners: p.structuralMemoryOwnersLocked(),
		runtimeEntries:   make([]runtimeAnalysisEntrySnapshot, 0, len(p.runtimeAnalysis)+len(p.previousRuntimeAnalysis)),
	}
	appendRuntimeAnalysisEntrySnapshots(&snapshot.runtimeEntries, p.runtimeAnalysis)
	appendRuntimeAnalysisEntrySnapshots(&snapshot.runtimeEntries, p.previousRuntimeAnalysis)
	return snapshot
}

func appendRuntimeAnalysisEntrySnapshots(snapshot *[]runtimeAnalysisEntrySnapshot, values map[string]any) {
	for _, value := range values {
		entry, ok := value.(*runtimeAnalysisEntry)
		if !ok || entry == nil {
			continue
		}
		*snapshot = append(*snapshot, runtimeAnalysisEntrySnapshot{
			value:        entry.value,
			rootIdentity: entry,
		})
	}
}

func (p *ParsedDocument) estimateStructuralBytesLocked() int64 {
	return p.estimateStructuralBytesWithRevisionTextLocked(true)
}

func (p *ParsedDocument) estimateStructuralBytesWithoutRevisionTextLocked() int64 {
	return p.estimateStructuralBytesWithRevisionTextLocked(false)
}

func (p *ParsedDocument) estimateStructuralBytesWithRevisionTextLocked(includeRevisionText bool) int64 {
	bytes := int64(256)
	bytes = saturatingAddBytes(bytes, estimateParsedStringBytes(p.URI))
	if includeRevisionText {
		bytes = saturatingAddBytes(bytes, estimateParsedStringBytes(p.Text))
	}
	bytes = saturatingAddBytes(bytes, estimateParsedStringBytes(string(p.DefaultLanguage)))
	if includeRevisionText {
		bytes = saturatingAddBytes(bytes, estimateParsedStringBytes(p.previousRevisionText))
	}
	bytes = saturatingAddBytes(bytes, saturatingMultiplyBytes(int64(cap(p.Regions)), 96))
	for _, region := range p.Regions {
		bytes = saturatingAddBytes(bytes, estimateParsedStringBytes(string(region.Kind)))
		bytes = saturatingAddBytes(bytes, estimateParsedStringBytes(string(region.Language)))
	}
	bytes = saturatingAddBytes(bytes, saturatingMultiplyBytes(int64(cap(p.Includes)), 128))
	for _, include := range p.Includes {
		bytes = saturatingAddBytes(bytes, estimateParsedStringBytes(include.Path))
		bytes = saturatingAddBytes(bytes, estimateParsedStringBytes(include.Mode))
	}
	bytes = saturatingAddBytes(bytes, saturatingMultiplyBytes(int64(cap(p.Errors)), 80))
	for _, parseError := range p.Errors {
		bytes = saturatingAddBytes(bytes, estimateParsedStringBytes(parseError.Message))
	}
	bytes = saturatingAddBytes(bytes, saturatingMultiplyBytes(int64(cap(p.ChangeImpact.Languages)), 24))
	for _, language := range p.ChangeImpact.Languages {
		bytes = saturatingAddBytes(bytes, estimateParsedStringBytes(string(language)))
	}
	bytes = saturatingAddBytes(bytes, estimateParsedStringBytes(p.ChangeImpact.Replacement))
	bytes = saturatingAddBytes(bytes, estimateParsedAnalysisMapBytes(p.Analysis))
	bytes = saturatingAddBytes(bytes, estimateParsedRuntimeMapStorageBytes(p.runtimeAnalysis))
	bytes = saturatingAddBytes(bytes, estimateParsedRuntimeMapStorageBytes(p.previousRuntimeAnalysis))
	return bytes
}

func (p *ParsedDocument) structuralMemoryOwnersLocked() map[any]int64 {
	owners := make(map[any]int64, 2)
	for _, text := range []string{p.Text, p.previousRevisionText} {
		identity, ok := runtimeValueBackingIdentity(reflect.ValueOf(text))
		if !ok {
			continue
		}
		bytes := estimateParsedStringBytes(text)
		if current, exists := owners[identity]; !exists || bytes > current {
			owners[identity] = bytes
		}
	}
	return owners
}

func estimateParsedStringBytes(value string) int64 {
	if value == "" {
		return 0
	}
	return saturatingAddBytes(saturatingMultiplyBytes(int64(len(value)), 2), 16)
}

func saturatingMultiplyBytes(left, right int64) int64 {
	if left <= 0 || right <= 0 {
		return 0
	}
	if left > math.MaxInt64/right {
		return math.MaxInt64
	}
	return left * right
}

func saturatingAddBytes(total, addition int64) int64 {
	if total < 0 {
		total = 0
	}
	if addition <= 0 {
		return total
	}
	if addition > math.MaxInt64-total {
		return math.MaxInt64
	}
	return total + addition
}

func estimateParsedAnalysisMapBytes(values map[string]json.RawMessage) int64 {
	if len(values) == 0 {
		return 0
	}
	bytes := int64(128)
	bytes = saturatingAddBytes(bytes, saturatingMultiplyBytes(int64(len(values)), 32))
	for key, payload := range values {
		bytes = saturatingAddBytes(bytes, estimateParsedStringBytes(key))
		bytes = saturatingAddBytes(bytes, int64(len(payload)))
		bytes = saturatingAddBytes(bytes, 24)
	}
	return bytes
}

func estimateParsedRuntimeMapStorageBytes(values map[string]any) int64 {
	if len(values) == 0 {
		return 0
	}
	bytes := int64(128)
	bytes = saturatingAddBytes(bytes, saturatingMultiplyBytes(int64(len(values)), 32))
	for key := range values {
		bytes = saturatingAddBytes(bytes, estimateParsedStringBytes(key))
		bytes = saturatingAddBytes(bytes, 16)
	}
	return bytes
}

func collectRuntimeAnalysisMemoryOwners(owners map[any]int64, entries []runtimeAnalysisEntrySnapshot) []runtimeAnalysisOwnerGeneration {
	var generations []runtimeAnalysisOwnerGeneration
	for _, entry := range entries {
		valueOwners, valueGenerations := runtimeAnalysisValueMemoryOwners(entry.value, entry.rootIdentity)
		for identity, bytes := range valueOwners {
			addRuntimeAnalysisOwner(owners, identity, bytes)
		}
		generations = append(generations, valueGenerations...)
	}
	return generations
}

// runtimeAnalysisBackingIdentity identifies one immutable reference backing.
// The shape fields avoid conflating distinct views whose ranges are not known
// to overlap, while retaining exact identity for unchanged copied fields.
type runtimeAnalysisBackingIdentity struct {
	kind     reflect.Kind
	typeKey  reflect.Type
	pointer  uintptr
	length   int
	capacity int
}

type runtimeValueVisit struct {
	backing runtimeAnalysisBackingIdentity
}

func runtimeAnalysisValueMemoryOwners(value any, rootIdentity any) (map[any]int64, []runtimeAnalysisOwnerGeneration) {
	root := reflect.ValueOf(value)
	if !root.IsValid() {
		return nil, nil
	}
	owners := make(map[any]int64)
	seen := make(map[runtimeValueVisit]struct{})
	var generations []runtimeAnalysisOwnerGeneration
	if provider, ok := runtimeValueMemoryOwnerProvider(root); ok {
		appendRuntimeAnalysisOwnerGeneration(&generations, root)
		return runtimeAnalysisProviderMemoryOwners(provider), generations
	}
	complete, hasComplete := runtimeValueCompleteEstimator(root)
	exclusive, hasExclusive := runtimeValueExclusiveEstimator(root)
	if hasComplete {
		appendRuntimeAnalysisOwnerGeneration(&generations, root)
		identity := rootIdentity
		if backing, ok := runtimeValueBackingIdentity(root); ok {
			identity = backing
		}
		if complete < 0 {
			complete = 0
		}
		addRuntimeAnalysisOwner(owners, identity, saturatingAddBytes(complete, 24))
		return owners, generations
	}

	switch root.Kind() {
	case reflect.Chan, reflect.Ptr, reflect.UnsafePointer:
		if identity, ok := runtimeValueBackingIdentity(root); ok {
			bytes := saturatingAddBytes(int64(root.Type().Size()), 24)
			if hasExclusive {
				bytes = saturatingAddBytes(bytes, exclusive)
			}
			addRuntimeAnalysisOwner(owners, identity, bytes)
			return owners, generations
		}
		bytes := saturatingAddBytes(int64(root.Type().Size()), 24)
		if hasExclusive {
			bytes = saturatingAddBytes(bytes, exclusive)
		}
		addRuntimeAnalysisOwner(owners, rootIdentity, bytes)
		return owners, generations
	case reflect.String, reflect.Map, reflect.Slice:
		if identity, ok := runtimeValueBackingIdentity(root); ok {
			collectRuntimeReferencedValue(root, owners, seen, rootIdentity, true)
			if hasExclusive {
				addRuntimeAnalysisOwnerBytes(owners, identity, exclusive)
			}
			return owners, generations
		}
	}

	addRuntimeAnalysisOwner(owners, rootIdentity, saturatingAddBytes(int64(root.Type().Size()), 24))
	appendRuntimeAnalysisOwnerGeneration(&generations, root)
	collectRuntimeReferencedValue(root, owners, seen, rootIdentity, false)
	if hasExclusive {
		addRuntimeAnalysisOwnerBytes(owners, rootIdentity, exclusive)
	}
	return owners, generations
}

func runtimeValueMemoryOwnerProvider(value reflect.Value) (RuntimeAnalysisMemoryOwnerProvider, bool) {
	if !value.IsValid() || !value.CanInterface() {
		return nil, false
	}
	provider, ok := value.Interface().(RuntimeAnalysisMemoryOwnerProvider)
	if !ok {
		return nil, false
	}
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice, reflect.UnsafePointer:
		if runtimeValueIsNil(value) {
			return nil, true
		}
	}
	return provider, true
}

func runtimeAnalysisProviderMemoryOwners(provider RuntimeAnalysisMemoryOwnerProvider) map[any]int64 {
	if provider == nil {
		return nil
	}
	value := reflect.ValueOf(provider)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice, reflect.UnsafePointer:
		if runtimeValueIsNil(value) {
			return nil
		}
	}
	owners := make(map[any]int64)
	var headerIdentity any
	headerIdentitySet := false
	for _, owner := range provider.RuntimeAnalysisMemoryOwnerSet() {
		// Non-positive estimates do not represent retained storage and must not
		// select the per-entry header identity.
		if owner.Bytes <= 0 || !runtimeAnalysisOwnerIdentityIsValid(owner.Identity) {
			continue
		}
		addRuntimeAnalysisOwner(owners, owner.Identity, owner.Bytes)
		if !headerIdentitySet {
			headerIdentity = owner.Identity
			headerIdentitySet = true
		}
	}
	if headerIdentitySet {
		addRuntimeAnalysisOwnerBytes(owners, headerIdentity, 24)
	}
	return owners
}

func runtimeAnalysisOwnerIdentityIsValid(identity any) (valid bool) {
	if identity == nil {
		return false
	}
	value := reflect.ValueOf(identity)
	if !value.IsValid() {
		return false
	}
	defer func() {
		if recover() != nil {
			valid = false
		}
	}()
	if !value.Comparable() {
		return false
	}
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice, reflect.UnsafePointer:
		return !value.IsNil()
	default:
		return true
	}
}

func appendRuntimeAnalysisOwnerGeneration(generations *[]runtimeAnalysisOwnerGeneration, value reflect.Value) {
	if generations == nil || !value.IsValid() || !value.CanInterface() {
		return
	}
	provider, ok := value.Interface().(RuntimeAnalysisMemoryOwnerGenerationProvider)
	if !ok {
		return
	}
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice, reflect.UnsafePointer:
		if runtimeValueIsNil(value) {
			return
		}
	}
	*generations = append(*generations, runtimeAnalysisOwnerGeneration{
		provider:   provider,
		generation: provider.RuntimeAnalysisMemoryOwnerGeneration(),
	})
}

func runtimeAnalysisOwnerGenerationsMatch(generations []runtimeAnalysisOwnerGeneration) bool {
	for _, snapshot := range generations {
		if snapshot.provider == nil || snapshot.provider.RuntimeAnalysisMemoryOwnerGeneration() != snapshot.generation {
			return false
		}
	}
	return true
}

func runtimeValueCompleteEstimator(value reflect.Value) (int64, bool) {
	if !value.IsValid() || !value.CanInterface() {
		return 0, false
	}
	estimator, ok := value.Interface().(interface{ EstimateBytes() int64 })
	if !ok {
		return 0, false
	}
	return estimator.EstimateBytes(), true
}

func runtimeValueExclusiveEstimator(value reflect.Value) (int64, bool) {
	if !value.IsValid() || !value.CanInterface() {
		return 0, false
	}
	estimator, ok := value.Interface().(RuntimeAnalysisExclusiveEstimator)
	if !ok {
		return 0, false
	}
	return estimator.EstimateExclusiveRuntimeBytes(), true
}

// runtimeAnalysisOwnerHandle is the exported form of a backing identity. It
// replaces the reflect.Type interface with the type descriptor's address, which
// is stable for the life of the process, so callers that collect owners from
// many documents hash plain words instead of an interface.
type runtimeAnalysisOwnerHandle struct {
	kind     reflect.Kind
	typeKey  uintptr
	pointer  uintptr
	length   int
	capacity int
}

// exportedMemoryOwnerIdentity converts reflected backing identities into
// cheaply hashable handles. Equal backings still compare equal.
func exportedMemoryOwnerIdentity(identity any) any {
	if backing, ok := identity.(runtimeAnalysisBackingIdentity); ok {
		handle := runtimeAnalysisOwnerHandle{kind: backing.kind, pointer: backing.pointer, length: backing.length, capacity: backing.capacity}
		if backing.typeKey != nil {
			handle.typeKey = reflect.ValueOf(backing.typeKey).Pointer()
		}
		return handle
	}
	return identity
}

func runtimeValueBackingIdentity(value reflect.Value) (runtimeAnalysisBackingIdentity, bool) {
	if !value.IsValid() {
		return runtimeAnalysisBackingIdentity{}, false
	}
	identity := runtimeAnalysisBackingIdentity{
		kind:    value.Kind(),
		typeKey: value.Type(),
	}
	switch value.Kind() {
	case reflect.String:
		if value.Len() == 0 {
			return runtimeAnalysisBackingIdentity{}, false
		}
		identity.length = value.Len()
		identity.pointer = uintptr(value.UnsafePointer())
	case reflect.Slice:
		if value.IsNil() || value.Cap() == 0 {
			return runtimeAnalysisBackingIdentity{}, false
		}
		identity.length = value.Len()
		identity.capacity = value.Cap()
		identity.pointer = uintptr(value.UnsafePointer())
	case reflect.Map, reflect.Chan, reflect.Ptr, reflect.UnsafePointer:
		if runtimeValueIsNil(value) {
			return runtimeAnalysisBackingIdentity{}, false
		}
		identity.pointer = uintptr(value.UnsafePointer())
	default:
		return runtimeAnalysisBackingIdentity{}, false
	}
	return identity, identity.pointer != 0
}

func runtimeValueIsNil(value reflect.Value) bool {
	if value.Kind() == reflect.UnsafePointer {
		return value.UnsafePointer() == nil
	}
	return value.IsNil()
}

func addRuntimeAnalysisOwner(owners map[any]int64, identity any, bytes int64) {
	if bytes <= 0 || !runtimeAnalysisOwnerIdentityIsValid(identity) {
		return
	}
	if current, exists := owners[identity]; !exists || bytes > current {
		owners[identity] = bytes
	}
}

func addRuntimeAnalysisOwnerBytes(owners map[any]int64, identity any, bytes int64) {
	if bytes <= 0 || !runtimeAnalysisOwnerIdentityIsValid(identity) {
		return
	}
	current := owners[identity]
	if current < 0 {
		current = 0
	}
	if bytes > math.MaxInt64-current {
		owners[identity] = math.MaxInt64
		return
	}
	owners[identity] = current + bytes
}

func collectRuntimeReferencedValue(value reflect.Value, owners map[any]int64, seen map[runtimeValueVisit]struct{}, inlineIdentity any, includeHeader bool) int64 {
	if !value.IsValid() {
		return 0
	}
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return 0
		}
		return collectRuntimeReferencedValue(value.Elem(), owners, seen, inlineIdentity, false)
	case reflect.Chan, reflect.Ptr, reflect.UnsafePointer:
		if runtimeValueIsNil(value) {
			return 0
		}
		identity, ok := runtimeValueBackingIdentity(value)
		if !ok {
			return 0
		}
		visit := runtimeValueVisit{backing: identity}
		if _, exists := seen[visit]; exists {
			return 0
		}
		seen[visit] = struct{}{}
		if estimator, ok := runtimeValueCompleteEstimator(value); ok {
			complete := estimator
			if complete < 0 {
				complete = 0
			}
			base := int64(value.Type().Size())
			if complete > base {
				extra := complete - base
				addRuntimeAnalysisOwnerBytes(owners, identity, saturatingAddBytes(extra, 16))
				return extra
			}
			return 0
		}
		if estimator, ok := runtimeValueExclusiveEstimator(value); ok && estimator > 0 {
			addRuntimeAnalysisOwnerBytes(owners, identity, estimator)
			return estimator
		}
		return 0
	case reflect.String:
		identity, ok := runtimeValueBackingIdentity(value)
		if !ok {
			return 0
		}
		visit := runtimeValueVisit{backing: identity}
		if _, exists := seen[visit]; exists {
			return 0
		}
		seen[visit] = struct{}{}
		bytes := saturatingAddBytes(saturatingMultiplyBytes(int64(value.Len()), 2), 16)
		if includeHeader {
			bytes = saturatingAddBytes(bytes, int64(value.Type().Size()))
		}
		addRuntimeAnalysisOwner(owners, identity, saturatingAddBytes(bytes, 16))
		if estimator, ok := runtimeValueExclusiveEstimator(value); ok && estimator > 0 {
			addRuntimeAnalysisOwnerBytes(owners, identity, estimator)
			bytes = saturatingAddBytes(bytes, estimator)
		}
		return bytes
	case reflect.Slice:
		identity, ok := runtimeValueBackingIdentity(value)
		if !ok {
			return 0
		}
		visit := runtimeValueVisit{backing: identity}
		if _, exists := seen[visit]; exists {
			return 0
		}
		seen[visit] = struct{}{}
		bytes := saturatingMultiplyBytes(int64(value.Cap()), int64(value.Type().Elem().Size()))
		if includeHeader {
			bytes = saturatingAddBytes(bytes, int64(value.Type().Size()))
		}
		addRuntimeAnalysisOwner(owners, identity, saturatingAddBytes(bytes, 16))
		if estimator, ok := runtimeValueExclusiveEstimator(value); ok && estimator > 0 {
			addRuntimeAnalysisOwnerBytes(owners, identity, estimator)
			bytes = saturatingAddBytes(bytes, estimator)
		}
		for index := 0; index < value.Len(); index++ {
			bytes = saturatingAddBytes(bytes, collectRuntimeReferencedValue(value.Index(index), owners, seen, inlineIdentity, false))
		}
		return bytes
	case reflect.Map:
		identity, ok := runtimeValueBackingIdentity(value)
		if !ok {
			return 0
		}
		visit := runtimeValueVisit{backing: identity}
		if _, exists := seen[visit]; exists {
			return 0
		}
		seen[visit] = struct{}{}
		bytes := int64(64)
		entryBytes := saturatingAddBytes(int64(value.Type().Key().Size()), int64(value.Type().Elem().Size()))
		bytes = saturatingAddBytes(bytes, saturatingMultiplyBytes(int64(value.Len()), entryBytes))
		if includeHeader {
			bytes = saturatingAddBytes(bytes, int64(value.Type().Size()))
		}
		addRuntimeAnalysisOwner(owners, identity, saturatingAddBytes(bytes, 16))
		if estimator, ok := runtimeValueExclusiveEstimator(value); ok && estimator > 0 {
			addRuntimeAnalysisOwnerBytes(owners, identity, estimator)
			bytes = saturatingAddBytes(bytes, estimator)
		}
		iterator := value.MapRange()
		for iterator.Next() {
			bytes = saturatingAddBytes(bytes, collectRuntimeReferencedValue(iterator.Key(), owners, seen, inlineIdentity, false))
			bytes = saturatingAddBytes(bytes, collectRuntimeReferencedValue(iterator.Value(), owners, seen, inlineIdentity, false))
		}
		return bytes
	case reflect.Struct:
		if estimator, ok := runtimeValueCompleteEstimator(value); ok {
			complete := estimator
			if complete < 0 {
				complete = 0
			}
			base := int64(value.Type().Size())
			if complete > base {
				extra := complete - base
				addRuntimeAnalysisOwnerBytes(owners, inlineIdentity, saturatingAddBytes(extra, 16))
				return extra
			}
			return 0
		}
		var bytes int64
		for index := 0; index < value.NumField(); index++ {
			bytes = saturatingAddBytes(bytes, collectRuntimeReferencedValue(value.Field(index), owners, seen, inlineIdentity, false))
		}
		if estimator, ok := runtimeValueExclusiveEstimator(value); ok && estimator > 0 {
			addRuntimeAnalysisOwnerBytes(owners, inlineIdentity, estimator)
			bytes = saturatingAddBytes(bytes, estimator)
		}
		return bytes
	case reflect.Array:
		var bytes int64
		for index := 0; index < value.Len(); index++ {
			bytes = saturatingAddBytes(bytes, collectRuntimeReferencedValue(value.Index(index), owners, seen, inlineIdentity, false))
		}
		return bytes
	default:
		// Scalar values are already represented by the containing struct, array,
		// map, or slice backing. Pointer-backed mutable service caches provide
		// their own synchronized EstimateBytes method above.
		return 0
	}
}

type Settings struct {
	DefaultLanguage string
}

func ParseDocument(uri, text string, settings Settings) *ParsedDocument {
	if isStandaloneVBScriptURI(uri) {
		return parseStandaloneVBScriptDocument(uri, text)
	}
	aspOpenFeatures := newASPOpenScanFeatures(text)
	aspOpenFeatures.quotes = &lineQuoteScanner{text: text}
	defaultLanguage := normalizeServerLanguage(settings.DefaultLanguage)
	if directiveLanguage, ok := aspDirectiveLanguageWithFeatures(text, aspOpenFeatures); ok {
		defaultLanguage = normalizeServerLanguage(directiveLanguage)
	}
	regions := make([]Region, 0, 16)
	errors := make([]ParseError, 0)
	cursor := 0
	for cursor < len(text) {
		open := indexASPOpenWithFeatures(text, cursor, aspOpenFeatures)
		if open == -1 {
			if cursor < len(text) {
				regions = append(regions, Region{Kind: RegionHTML, Language: LanguageHTML, Start: cursor, End: len(text), ContentStart: cursor, ContentEnd: len(text)})
			}
			break
		}
		if cursor < open {
			regions = append(regions, Region{Kind: RegionHTML, Language: LanguageHTML, Start: cursor, End: open, ContentStart: cursor, ContentEnd: open})
		}
		close := aspCloseDelimiter(text, open)
		if close == -1 {
			errors = append(errors, ParseError{Start: open, End: len(text), Message: "Expected closing %> before end of file."})
			regions = append(regions, Region{Kind: RegionASPBlock, Language: defaultLanguage, Start: open, End: len(text), ContentStart: open + 2, ContentEnd: len(text)})
			break
		}
		kind := RegionASPBlock
		language := defaultLanguage
		contentStart := open + 2
		if contentStart < len(text) {
			switch text[contentStart] {
			case '=':
				kind = RegionASPExpression
				contentStart++
			case '@':
				kind = RegionASPDirective
				language = LanguageASPDirective
				contentStart++
			}
		}
		regions = append(regions, Region{Kind: kind, Language: language, Start: open, End: close + 2, ContentStart: contentStart, ContentEnd: close})
		cursor = close + 2
	}
	regions = append(regions, scanHTMLScriptAndStyle(text, regions, defaultLanguage)...)
	doc := &ParsedDocument{
		URI:             uri,
		Text:            text,
		DefaultLanguage: defaultLanguage,
		Regions:         mergeRegions(regions),
		Errors:          errors,
	}
	if containsASCIIFold(text, "#include") {
		source := NewTextDocument(uri, "classic-asp", 0, text)
		doc.StoreRuntimeAnalysis(sourceDocumentAnalysisKey, source)
		doc.Includes = extractIncludes(source, text)
	}
	return doc
}

func parseStandaloneVBScriptDocument(uri, text string) *ParsedDocument {
	return &ParsedDocument{
		URI:             uri,
		Text:            text,
		DefaultLanguage: LanguageVBScript,
		Regions: []Region{{
			Kind:         RegionServerScript,
			Language:     LanguageVBScript,
			Start:        0,
			End:          len(text),
			ContentStart: 0,
			ContentEnd:   len(text),
		}},
	}
}

func isStandaloneVBScriptURI(uri string) bool {
	path := uri
	if parsed, err := url.Parse(uri); err == nil && parsed.Path != "" {
		path = parsed.Path
		if unescaped, err := url.PathUnescape(path); err == nil {
			path = unescaped
		}
	}
	return strings.HasSuffix(strings.ToLower(path), ".vbs")
}
