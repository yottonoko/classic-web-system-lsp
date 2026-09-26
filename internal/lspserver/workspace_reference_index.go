package lspserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

type workspaceReferenceCountSummary struct {
	Declarations                  int
	Reads                         int
	Writes                        int
	Calls                         int
	Crefs                         int
	ObjectInitializations         int
	CodeLensReferences            int
	CodeLensCalls                 int
	UnqualifiedCodeLensReferences int
	UnqualifiedCodeLensCalls      int
	Total                         int
}

type workspaceReferenceDocumentSegment struct {
	documentID          uint64
	documentKey         string
	name                string
	parsed              *core.ParsedDocument
	countFingerprint    string
	locationFingerprint string
	postings            []vbscript.ReferencePosting
	globalResolutions   []bool
	postingsLoaded      bool
	declarationRanges   map[lsp.Range]struct{}
	implicitAdjustments map[lsp.Range]workspaceReferenceCountSummary
	counts              workspaceReferenceCountSummary
	hydration           *workspaceReferenceSegmentHydration
}

type workspaceReferenceSegmentHydration struct {
	mu                sync.Mutex
	ready             bool
	postings          []vbscript.ReferencePosting
	globalResolutions []bool
	declarationRanges map[lsp.Range]struct{}
}

type workspaceEmbeddedClassSegment struct {
	documentKey string
	parsed      *core.ParsedDocument
	fingerprint string
	ranges      []lsp.Range
}

type workspaceEmbeddedClassDocumentRanges struct {
	URI    string
	Ranges []lsp.Range
}

type workspaceReferenceIndexEntry struct {
	documentID          uint64
	parsed              *core.ParsedDocument
	sourceHash          string
	countFingerprint    string
	locationFingerprint string
	embeddedFingerprint string
	names               []string
	segments            map[string]*workspaceReferenceDocumentSegment
	embeddedNames       []string
	embeddedSegments    map[string]*workspaceEmbeddedClassSegment
	countSummaryOnly    bool
	ticket              uint64
}

type workspaceReferenceIndexUpdate struct {
	ChangedDocuments           int
	SemanticUnchangedDocuments int
	CountAffectedNames         []string
	LocationAffectedNames      []string
	EmbeddedAffectedNames      []string
	AffectedNames              []string
}

// workspaceReferenceIndex is a document-segment inverted view of the persisted
// per-document reference shards. Expensive shard construction happens without
// holding mu; applying one document only rewrites postings for changed names.
type workspaceReferenceIndex struct {
	aggregateCache        workspaceReferenceAggregateCache
	mu                    sync.RWMutex
	documents             map[string]workspaceReferenceIndexEntry
	parsedDocuments       map[*core.ParsedDocument]string
	names                 map[string]map[uint64]*workspaceReferenceDocumentSegment
	embeddedClasses       map[string]map[string]*workspaceEmbeddedClassSegment
	deletedThrough        map[string]uint64
	sequence              atomic.Uint64
	nextDocumentID        uint64
	clearedThrough        atomic.Uint64
	countRevision         atomic.Uint64
	locationRevision      atomic.Uint64
	embeddedRevision      atomic.Uint64
	workers               *analysisWorkerPool
	embeddedBuildTestHook func(string)
}

func (index *workspaceReferenceIndex) setWorkerPool(pool *analysisWorkerPool) {
	if index == nil {
		return
	}
	index.mu.Lock()
	index.workers = pool
	index.mu.Unlock()
}

// revisionNumber changes only when indexed reference semantics change.
func (index *workspaceReferenceIndex) revisionNumber() uint64 {
	if index == nil {
		return 0
	}
	return index.countRevision.Load()
}

func (index *workspaceReferenceIndex) locationRevisionNumber() uint64 {
	if index == nil {
		return 0
	}
	return index.locationRevision.Load()
}

// semanticFingerprintForDocuments computes a stable key from already-indexed
// semantic fingerprints. It never reads or hashes source text. ok is false when
// any requested document has not been indexed yet.
func (index *workspaceReferenceIndex) semanticFingerprintForDocuments(candidates []*core.ParsedDocument) (fingerprint string, ok bool) {
	if index == nil {
		return "", false
	}
	type part struct {
		key         string
		fingerprint string
	}
	parts := make([]part, 0, len(candidates))
	index.mu.RLock()
	for _, candidate := range candidates {
		if candidate == nil {
			continue
		}
		key := index.parsedDocuments[candidate]
		if key == "" {
			key = workspacepkg.FileIdentityKeyFromURI(candidate.URI)
		}
		entry, found := index.documents[key]
		if !found {
			index.mu.RUnlock()
			return "", false
		}
		parts = append(parts, part{key: key, fingerprint: entry.countFingerprint})
	}
	index.mu.RUnlock()
	sort.Slice(parts, func(i, j int) bool { return parts[i].key < parts[j].key })
	hash := sha256.New()
	for _, item := range parts {
		_, _ = hash.Write([]byte(item.key))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(item.fingerprint))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil)), true
}

// semanticFingerprintsForNames returns one stable fingerprint per requested
// name without building a document-by-name matrix. The scope fingerprint is
// kept separately, so a missing segment is represented by its absence here.
func (index *workspaceReferenceIndex) semanticFingerprintsForNames(names []string, candidates []*core.ParsedDocument) map[string]string {
	return index.semanticFingerprintsForNamesContext(context.Background(), names, candidates)
}

func (index *workspaceReferenceIndex) semanticFingerprintsForNamesContext(ctx context.Context, names []string, candidates []*core.ParsedDocument) map[string]string {
	if ctx.Err() != nil {
		return nil
	}
	result := make(map[string]string, len(names))
	if index == nil || len(names) == 0 {
		return result
	}
	index.updateCountContext(ctx, candidates)
	if ctx.Err() != nil {
		return nil
	}
	requested := make(map[string]struct{}, len(names))
	for _, rawName := range names {
		requested[strings.ToLower(rawName)] = struct{}{}
	}
	type fingerprintPart struct {
		documentKey string
		fingerprint string
	}
	parts := make(map[string][]fingerprintPart, len(requested))
	index.mu.RLock()
	acceptedDocuments := make(map[uint64]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate == nil {
			continue
		}
		if key := index.parsedDocuments[candidate]; key != "" {
			if entry, ok := index.documents[key]; ok {
				acceptedDocuments[entry.documentID] = struct{}{}
			}
		}
	}
	for name := range requested {
		for _, segment := range index.names[name] {
			if segment != nil {
				if _, accepted := acceptedDocuments[segment.documentID]; accepted {
					parts[name] = append(parts[name], fingerprintPart{documentKey: segment.documentKey, fingerprint: segment.countFingerprint})
				}
			}
		}
	}
	index.mu.RUnlock()
	for name := range requested {
		nameParts := parts[name]
		sort.Slice(nameParts, func(i, j int) bool { return nameParts[i].documentKey < nameParts[j].documentKey })
		hash := sha256.New()
		for _, part := range nameParts {
			_, _ = hash.Write([]byte(part.documentKey))
			_, _ = hash.Write([]byte{0})
			_, _ = hash.Write([]byte(part.fingerprint))
			_, _ = hash.Write([]byte{0})
		}
		result[name] = hex.EncodeToString(hash.Sum(nil))
	}
	return result
}

func newWorkspaceReferenceIndex() *workspaceReferenceIndex {
	return &workspaceReferenceIndex{
		documents:       map[string]workspaceReferenceIndexEntry{},
		parsedDocuments: map[*core.ParsedDocument]string{},
		names:           map[string]map[uint64]*workspaceReferenceDocumentSegment{},
		embeddedClasses: map[string]map[string]*workspaceEmbeddedClassSegment{},
		deletedThrough:  map[string]uint64{},
	}
}

func (index *workspaceReferenceIndex) embeddedClassRanges(name string, candidates []*core.ParsedDocument) []workspaceEmbeddedClassDocumentRanges {
	if index == nil || name == "" || len(candidates) == 0 {
		return nil
	}
	index.update(candidates)
	index.mu.RLock()
	matching := index.embeddedClasses[name]
	result := make([]workspaceEmbeddedClassDocumentRanges, 0, len(matching))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate == nil {
			continue
		}
		key := index.parsedDocuments[candidate]
		if key == "" {
			key = workspacepkg.FileIdentityKeyFromURI(candidate.URI)
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		if segment := matching[key]; segment != nil {
			result = append(result, workspaceEmbeddedClassDocumentRanges{URI: segment.parsed.URI, Ranges: segment.ranges})
		}
	}
	index.mu.RUnlock()
	return result
}

func (index *workspaceReferenceIndex) documentsForName(name string, candidates []*core.ParsedDocument) []*core.ParsedDocument {
	if index == nil || len(candidates) == 0 {
		return candidates
	}
	index.update(candidates)
	name = strings.ToLower(name)
	index.mu.RLock()
	matching := index.names[name]
	result := make([]*core.ParsedDocument, 0, len(matching))
	for _, candidate := range candidates {
		if candidate == nil {
			continue
		}
		key := index.parsedDocuments[candidate]
		if key == "" {
			key = workspacepkg.FileIdentityKeyFromURI(candidate.URI)
		}
		if entry, ok := index.documents[key]; ok && matching[entry.documentID] != nil {
			result = append(result, candidate)
		}
	}
	index.mu.RUnlock()
	return result
}

// segmentsForName returns immutable per-document posting segments in candidate
// order. Callers may count or decode them in parallel without holding index.mu.
func (index *workspaceReferenceIndex) segmentsForName(name string, candidates []*core.ParsedDocument) []*workspaceReferenceDocumentSegment {
	return index.segmentsForNamesContext(context.Background(), []string{name}, candidates)[strings.ToLower(name)]
}

func (index *workspaceReferenceIndex) segmentsForNameContext(ctx context.Context, name string, candidates []*core.ParsedDocument) []*workspaceReferenceDocumentSegment {
	return index.segmentsForNamesContextMode(ctx, []string{name}, candidates, true)[strings.ToLower(name)]
}

// segmentsForNamesContext snapshots matching inverted-index segments in candidate
// order. Stable document IDs avoid a full-scope string map and a scope-sized
// scratch slice for every requested name.
func (index *workspaceReferenceIndex) segmentsForNamesContext(ctx context.Context, names []string, candidates []*core.ParsedDocument) map[string][]*workspaceReferenceDocumentSegment {
	return index.segmentsForNamesContextMode(ctx, names, candidates, false)
}

func (index *workspaceReferenceIndex) segmentsForNamesContextMode(ctx context.Context, names []string, candidates []*core.ParsedDocument, requireFull bool) map[string][]*workspaceReferenceDocumentSegment {
	result := make(map[string][]*workspaceReferenceDocumentSegment, len(names))
	if index == nil || len(candidates) == 0 || len(names) == 0 {
		return result
	}
	index.updateContextMode(ctx, candidates, requireFull)
	if ctx.Err() != nil {
		return result
	}
	index.mu.RLock()
	documentIDs := make([]uint64, 0, len(candidates))
	// A bitset is cheapest while IDs remain close to the live scope. Fall back
	// to scope-bounded maps after heavy add/remove churn leaves the ID space sparse.
	useSeenBits := index.nextDocumentID <= uint64(max(64, len(candidates)*8))
	var seenBits []uint64
	var seenMap map[uint64]struct{}
	if useSeenBits {
		seenBits = make([]uint64, (index.nextDocumentID+64)/64)
	} else {
		seenMap = make(map[uint64]struct{}, len(candidates))
	}
	for _, candidate := range candidates {
		if candidate == nil {
			continue
		}
		key := index.parsedDocuments[candidate]
		if key == "" {
			key = workspacepkg.FileIdentityKeyFromURI(candidate.URI)
		}
		entry, found := index.documents[key]
		if !found {
			continue
		}
		if useSeenBits {
			word := entry.documentID / 64
			bit := uint64(1) << (entry.documentID % 64)
			if seenBits[word]&bit != 0 {
				continue
			}
			seenBits[word] |= bit
		} else {
			if _, duplicate := seenMap[entry.documentID]; duplicate {
				continue
			}
			seenMap[entry.documentID] = struct{}{}
		}
		documentIDs = append(documentIDs, entry.documentID)
	}
	var candidateOrdinalBits []int32
	var candidateOrdinals map[uint64]int
	for _, rawName := range names {
		name := strings.ToLower(rawName)
		if _, exists := result[name]; exists {
			continue
		}
		matching := index.names[name]
		if len(matching) == 0 {
			result[name] = nil
			continue
		}
		segments := make([]*workspaceReferenceDocumentSegment, 0, min(len(matching), len(documentIDs)))
		// Sparse postings are cheaper to intersect from the postings side. Dense
		// postings retain the direct candidate-order scan and avoid sorting.
		if len(matching)*4 < len(documentIDs) {
			if useSeenBits && candidateOrdinalBits == nil {
				candidateOrdinalBits = make([]int32, index.nextDocumentID+1)
				for ordinal, documentID := range documentIDs {
					candidateOrdinalBits[documentID] = int32(ordinal + 1)
				}
			} else if !useSeenBits && candidateOrdinals == nil {
				candidateOrdinals = make(map[uint64]int, len(documentIDs))
				for ordinal, documentID := range documentIDs {
					candidateOrdinals[documentID] = ordinal
				}
			}
			type orderedSegment struct {
				ordinal int
				segment *workspaceReferenceDocumentSegment
			}
			ordered := make([]orderedSegment, 0, len(matching))
			for documentID, segment := range matching {
				if segment == nil {
					continue
				}
				if useSeenBits {
					if documentID < uint64(len(candidateOrdinalBits)) && candidateOrdinalBits[documentID] != 0 {
						ordered = append(ordered, orderedSegment{ordinal: int(candidateOrdinalBits[documentID] - 1), segment: segment})
					}
				} else if ordinal, accepted := candidateOrdinals[documentID]; accepted {
					ordered = append(ordered, orderedSegment{ordinal: ordinal, segment: segment})
				}
			}
			sort.Slice(ordered, func(i, j int) bool { return ordered[i].ordinal < ordered[j].ordinal })
			for _, item := range ordered {
				segments = append(segments, item.segment)
			}
		} else {
			for _, documentID := range documentIDs {
				if segment := matching[documentID]; segment != nil {
					segments = append(segments, segment)
				}
			}
		}
		result[name] = segments
	}
	index.mu.RUnlock()
	return result
}

func (index *workspaceReferenceIndex) clear() {
	if index == nil {
		return
	}
	clearTicket := index.sequence.Add(1)
	index.clearedThrough.Store(clearTicket)
	index.countRevision.Add(1)
	index.locationRevision.Add(1)
	index.embeddedRevision.Add(1)
	index.mu.Lock()
	index.documents = map[string]workspaceReferenceIndexEntry{}
	index.parsedDocuments = map[*core.ParsedDocument]string{}
	index.names = map[string]map[uint64]*workspaceReferenceDocumentSegment{}
	index.embeddedClasses = map[string]map[string]*workspaceEmbeddedClassSegment{}
	index.deletedThrough = map[string]uint64{}
	index.nextDocumentID = 0
	index.aggregateCache.clear()
	index.mu.Unlock()
}

func (index *workspaceReferenceIndex) estimateMemory() (int64, int) {
	if index == nil {
		return 0, 0
	}
	index.mu.RLock()
	defer index.mu.RUnlock()
	var bytes int64
	segments := 0
	for key, entry := range index.documents {
		bytes += int64(len(key)+len(entry.sourceHash)+len(entry.countFingerprint)+len(entry.locationFingerprint)) * 2
		bytes += int64(len(entry.names)) * 32
		for name, segment := range entry.segments {
			if segment == nil {
				continue
			}
			segments++
			bytes += int64(len(name)+len(segment.documentKey)+len(segment.countFingerprint)+len(segment.locationFingerprint))*2 + 160
			bytes += int64(len(segment.postings))*128 + int64(len(segment.globalResolutions)) + int64(len(segment.declarationRanges))*48
			bytes += int64(len(segment.implicitAdjustments)) * 96
			if hydration := segment.hydration; hydration != nil {
				hydration.mu.Lock()
				bytes += 64 + int64(len(hydration.postings))*128 + int64(len(hydration.globalResolutions)) + int64(len(hydration.declarationRanges))*48
				hydration.mu.Unlock()
			}
		}
		bytes += int64(len(entry.embeddedNames)) * 32
		for name, segment := range entry.embeddedSegments {
			if segment == nil {
				continue
			}
			bytes += int64(len(name)+len(segment.documentKey)+len(segment.fingerprint))*2 + int64(len(segment.ranges))*32 + 96
		}
	}
	return bytes + index.aggregateCache.estimateBytes(), segments
}
