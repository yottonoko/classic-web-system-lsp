package lspserver

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"sort"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func (index *workspaceReferenceIndex) update(candidates []*core.ParsedDocument) workspaceReferenceIndexUpdate {
	return index.updateContext(context.Background(), candidates)
}

func (index *workspaceReferenceIndex) updateContext(ctx context.Context, candidates []*core.ParsedDocument) workspaceReferenceIndexUpdate {
	return index.updateContextMode(ctx, candidates, true)
}

func (index *workspaceReferenceIndex) updateCountContext(ctx context.Context, candidates []*core.ParsedDocument) workspaceReferenceIndexUpdate {
	return index.updateContextMode(ctx, candidates, false)
}

func (index *workspaceReferenceIndex) updateContextMode(ctx context.Context, candidates []*core.ParsedDocument, requireFull bool) workspaceReferenceIndexUpdate {
	update, prepared := index.prepareContextMode(ctx, candidates, requireFull)
	if prepared == nil {
		return update
	}
	return index.applyPrepared(prepared)
}

// prepareContextMode builds the immutable index entries without mutating the
// shared inverted index. Callers that must coordinate another publication can
// apply the returned entries under that publication's lock.
func (index *workspaceReferenceIndex) prepareContextMode(ctx context.Context, candidates []*core.ParsedDocument, requireFull bool) (workspaceReferenceIndexUpdate, []workspaceReferencePreparedDocument) {
	return index.prepareContextModeWithShards(ctx, candidates, nil, requireFull)
}

func (index *workspaceReferenceIndex) prepareContextModeWithShards(ctx context.Context, candidates []*core.ParsedDocument, shards map[*core.ParsedDocument]*vbscript.ReferenceShard, requireFull bool) (workspaceReferenceIndexUpdate, []workspaceReferencePreparedDocument) {
	return index.prepareContextModeWithAnalysis(ctx, candidates, shards, nil, requireFull)
}

// prepareContextModeWithAnalysis is prepareContextModeWithShards with optional
// analysis sources. A source is a parsed revision of the same text whose
// runtime analysis is already warm; it is only read, and the prepared entries
// still reference the candidate document.
func (index *workspaceReferenceIndex) prepareContextModeWithAnalysis(ctx context.Context, candidates []*core.ParsedDocument, shards map[*core.ParsedDocument]*vbscript.ReferenceShard, analysisSources map[*core.ParsedDocument]*core.ParsedDocument, requireFull bool) (workspaceReferenceIndexUpdate, []workspaceReferencePreparedDocument) {
	if index == nil || len(candidates) == 0 {
		return workspaceReferenceIndexUpdate{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	index.mu.RLock()
	allCurrent := true
	for _, candidate := range candidates {
		if candidate == nil {
			continue
		}
		key, found := index.parsedDocuments[candidate]
		if !found || requireFull && index.documents[key].countSummaryOnly {
			allCurrent = false
			break
		}
	}
	index.mu.RUnlock()
	if allCurrent {
		return workspaceReferenceIndexUpdate{SemanticUnchangedDocuments: len(candidates)}, nil
	}

	type pendingDocument struct {
		candidate *core.ParsedDocument
		analysis  *core.ParsedDocument
		shard     *vbscript.ReferenceShard
	}
	pending := make([]pendingDocument, 0, len(candidates))
	index.mu.RLock()
	for _, candidate := range candidates {
		if candidate == nil {
			continue
		}
		key, found := index.parsedDocuments[candidate]
		if !found || requireFull && index.documents[key].countSummaryOnly {
			analysis := candidate
			if source := analysisSources[candidate]; sameParsedAnalysisSource(candidate, source) {
				analysis = source
			}
			pending = append(pending, pendingDocument{candidate: candidate, analysis: analysis, shard: shards[candidate]})
		}
	}
	index.mu.RUnlock()
	if len(pending) == 0 {
		return workspaceReferenceIndexUpdate{SemanticUnchangedDocuments: len(candidates)}, nil
	}
	ticket := index.sequence.Add(1)
	prepared := make([]workspaceReferencePreparedDocument, len(pending))
	index.mu.RLock()
	pool := index.workers
	embeddedBuildTestHook := index.embeddedBuildTestHook
	index.mu.RUnlock()
	if pool == nil {
		workers := defaultAnalysisWorkers()
		pool = &analysisWorkerPool{
			workers: workers, slots: make(chan struct{}, workers),
			bulkSlots: make(chan struct{}, max(1, workers-1)),
		}
	}
	pool.parallelForBulk(ctx, len(pending), func(workerCtx context.Context, position int) {
		if workerCtx.Err() != nil {
			return
		}
		candidate := pending[position].candidate
		key := workspacepkg.FileIdentityKeyFromURI(candidate.URI)
		sourceHash := workspacepkg.DiskContentHash(candidate.Text)

		index.mu.RLock()
		current, found := index.documents[key]
		index.mu.RUnlock()
		if found && current.sourceHash == sourceHash && !(requireFull && current.countSummaryOnly) {
			prepared[position] = workspaceReferencePreparedDocument{key: key, parsed: candidate, sourceHash: sourceHash, unchangedSource: true, ticket: ticket}
			return
		}
		if workerCtx.Err() != nil {
			return
		}
		analysis := pending[position].analysis
		shard := pending[position].shard
		if shard == nil {
			shard = vbscript.BuildReferenceShard(analysis)
		}
		if requireFull {
			if embeddedBuildTestHook != nil {
				embeddedBuildTestHook(key)
			}
			prepared[position] = prepareWorkspaceReferenceDocumentFrom(key, candidate, analysis, sourceHash, shard, ticket)
		} else {
			prepared[position] = prepareWorkspaceReferenceCountDocumentFrom(key, candidate, analysis, sourceHash, shard, ticket)
		}
	})
	if ctx.Err() != nil {
		return workspaceReferenceIndexUpdate{}, nil
	}
	return workspaceReferenceIndexUpdate{}, prepared
}

func (index *workspaceReferenceIndex) seedPersistedCountSummary(parsed *core.ParsedDocument, sourceHash string, persisted map[string]persistedWorkspaceReferenceCountSummary) workspaceReferenceIndexUpdate {
	if index == nil {
		return workspaceReferenceIndexUpdate{}
	}
	return index.seedPersistedCountSummaryWithTicket(parsed, sourceHash, persisted, index.sequence.Add(1))
}

func (index *workspaceReferenceIndex) seedPersistedCountSummaryWithTicket(parsed *core.ParsedDocument, sourceHash string, persisted map[string]persistedWorkspaceReferenceCountSummary, ticket uint64) workspaceReferenceIndexUpdate {
	if index == nil || parsed == nil || len(persisted) == 0 {
		return workspaceReferenceIndexUpdate{}
	}
	key := workspacepkg.FileIdentityKeyFromURI(parsed.URI)
	index.mu.RLock()
	current, found := index.documents[key]
	index.mu.RUnlock()
	if found && current.sourceHash == sourceHash && (!current.countSummaryOnly || current.parsed == parsed) {
		return workspaceReferenceIndexUpdate{SemanticUnchangedDocuments: 1}
	}
	names := make([]string, 0, len(persisted))
	segments := make(map[string]*workspaceReferenceDocumentSegment, len(persisted))
	documentHash := sha256.New()
	for rawName, stored := range persisted {
		name := strings.ToLower(rawName)
		if name == "" || stored.CountFingerprint == "" {
			continue
		}
		names = append(names, name)
		declarationRanges := make(map[lsp.Range]struct{}, len(stored.DeclarationRanges))
		for _, item := range stored.DeclarationRanges {
			declarationRanges[item] = struct{}{}
		}
		adjustments := make(map[lsp.Range]workspaceReferenceCountSummary, len(stored.ImplicitAdjustments))
		for _, item := range stored.ImplicitAdjustments {
			adjustments[item.Range] = workspaceReferenceCountSummary{
				CodeLensReferences: item.CodeLensReferences, CodeLensCalls: item.CodeLensCalls,
				UnqualifiedCodeLensReferences: item.UnqualifiedCodeLensReferences,
				UnqualifiedCodeLensCalls:      item.UnqualifiedCodeLensCalls,
			}
		}
		segments[name] = &workspaceReferenceDocumentSegment{
			documentKey: key, name: name, parsed: parsed,
			countFingerprint: stored.CountFingerprint, locationFingerprint: stored.LocationFingerprint,
			declarationRanges: declarationRanges, implicitAdjustments: adjustments, counts: stored.Counts,
			hydration: &workspaceReferenceSegmentHydration{},
		}
	}
	sort.Strings(names)
	for _, name := range names {
		_, _ = documentHash.Write([]byte(name))
		_, _ = documentHash.Write([]byte{0})
		_, _ = documentHash.Write([]byte(segments[name].countFingerprint))
	}
	return index.applyPrepared([]workspaceReferencePreparedDocument{{
		key: key, parsed: parsed, sourceHash: sourceHash,
		countFingerprint: hex.EncodeToString(documentHash.Sum(nil)), locationFingerprint: "persisted-count-summary",
		names: names, segments: segments, embeddedSegments: map[string]*workspaceEmbeddedClassSegment{},
		countSummaryOnly: true, ticket: ticket,
	}})
}

type workspaceReferencePreparedDocument struct {
	key                 string
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
	unchangedSource     bool
	ticket              uint64
}

// sameParsedAnalysisSource reports whether source can stand in for parsed when
// deriving source-only analysis facts.
func sameParsedAnalysisSource(parsed, source *core.ParsedDocument) bool {
	return source != nil && parsed != nil && source != parsed && source.URI == parsed.URI &&
		source.DefaultLanguage == parsed.DefaultLanguage && source.Text == parsed.Text &&
		len(source.Regions) == len(parsed.Regions)
}

func prepareWorkspaceReferenceDocument(key string, parsed *core.ParsedDocument, sourceHash string, shard *vbscript.ReferenceShard, ticket uint64) workspaceReferencePreparedDocument {
	return prepareWorkspaceReferenceDocumentFrom(key, parsed, parsed, sourceHash, shard, ticket)
}

// prepareWorkspaceReferenceDocumentFrom derives facts from analysis and records
// parsed as the owner of the prepared segments.
func prepareWorkspaceReferenceDocumentFrom(key string, parsed, analysis *core.ParsedDocument, sourceHash string, shard *vbscript.ReferenceShard, ticket uint64) workspaceReferencePreparedDocument {
	prepared := prepareWorkspaceReferenceCountDocumentFrom(key, parsed, analysis, sourceHash, shard, ticket)
	locationHash := sha256.New()
	for _, name := range prepared.names {
		segment := prepared.segments[name]
		segment.postings = shard.Postings[name]
		segment.globalResolutions = shard.GlobalResolutionsFor(name)
		segment.postingsLoaded = true
		_, _ = locationHash.Write([]byte(name))
		_, _ = locationHash.Write([]byte{0})
		_, _ = locationHash.Write([]byte(segment.locationFingerprint))
	}
	prepared.locationFingerprint = hex.EncodeToString(locationHash.Sum(nil))
	prepared.embeddedNames, prepared.embeddedSegments, prepared.embeddedFingerprint = prepareWorkspaceEmbeddedClassSegmentsFrom(key, parsed, analysis)
	prepared.countSummaryOnly = false
	return prepared
}

func prepareWorkspaceReferenceCountDocument(key string, parsed *core.ParsedDocument, sourceHash string, shard *vbscript.ReferenceShard, ticket uint64) workspaceReferencePreparedDocument {
	return prepareWorkspaceReferenceCountDocumentFrom(key, parsed, parsed, sourceHash, shard, ticket)
}

func prepareWorkspaceReferenceCountDocumentFrom(key string, parsed, analysis *core.ParsedDocument, sourceHash string, shard *vbscript.ReferenceShard, ticket uint64) workspaceReferencePreparedDocument {
	names := shard.NormalizedNames()
	documentFacts := vbReferenceDocumentIndexForShard(analysis, shard)
	countHash := sha256.New()
	locationHash := sha256.New()
	segments := make(map[string]*workspaceReferenceDocumentSegment, len(names))
	for _, name := range names {
		summary, _ := shard.SummaryFor(name)
		postings := shard.Postings[name]
		globalResolutions := shard.GlobalResolutionsFor(name)
		declarationRanges := documentFacts.declarationRanges[name]
		counts := summarizeWorkspaceReferencePostingsWithResolutions(postings, globalResolutions, declarationRanges)
		countFingerprint, locationFingerprint := workspaceReferenceSegmentFingerprints(summary.CountFingerprint, summary.LocationFingerprint, counts, declarationRanges)
		segments[name] = &workspaceReferenceDocumentSegment{
			documentKey:         key,
			name:                name,
			parsed:              parsed,
			countFingerprint:    countFingerprint,
			locationFingerprint: locationFingerprint,
			declarationRanges:   declarationRanges,
			counts:              counts,
			hydration:           &workspaceReferenceSegmentHydration{},
		}
		_, _ = countHash.Write([]byte(name))
		_, _ = countHash.Write([]byte{0})
		_, _ = countHash.Write([]byte(countFingerprint))
		_, _ = locationHash.Write([]byte(name))
		_, _ = locationHash.Write([]byte{0})
		_, _ = locationHash.Write([]byte(locationFingerprint))
	}
	for _, declaration := range implicitVBDeclarations(analysis) {
		if declaration.Local {
			continue
		}
		name := strings.ToLower(declaration.Name)
		segment := segments[name]
		if segment == nil {
			continue
		}
		for _, posting := range shard.Postings[name] {
			_, excluded := documentFacts.declarationRanges[name][posting.Range]
			if posting.Range != declaration.Range || excluded || posting.HasRole(vbscript.ReferenceRoleDeclaration) || posting.HasRole(vbscript.ReferenceRoleCref) || posting.HasRole(vbscript.ReferenceRoleObjectInitialization) {
				continue
			}
			if segment.implicitAdjustments == nil {
				segment.implicitAdjustments = map[lsp.Range]workspaceReferenceCountSummary{}
			}
			adjustment := workspaceReferenceCountSummary{CodeLensReferences: 1, UnqualifiedCodeLensReferences: 1}
			if posting.HasRole(vbscript.ReferenceRoleCall) {
				adjustment.CodeLensCalls = 1
				adjustment.UnqualifiedCodeLensCalls = 1
			}
			segment.implicitAdjustments[declaration.Range] = adjustment
			break
		}
	}
	countFingerprint := hex.EncodeToString(countHash.Sum(nil))
	locationFingerprint := hex.EncodeToString(locationHash.Sum(nil))
	return workspaceReferencePreparedDocument{
		key: key, parsed: parsed, sourceHash: sourceHash,
		countFingerprint: countFingerprint, locationFingerprint: locationFingerprint,
		names: names, segments: segments, embeddedSegments: map[string]*workspaceEmbeddedClassSegment{},
		countSummaryOnly: true, ticket: ticket,
	}
}

func prepareWorkspaceEmbeddedClassSegments(key string, parsed *core.ParsedDocument) ([]string, map[string]*workspaceEmbeddedClassSegment, string) {
	return prepareWorkspaceEmbeddedClassSegmentsFrom(key, parsed, parsed)
}

func prepareWorkspaceEmbeddedClassSegmentsFrom(key string, parsed, analysis *core.ParsedDocument) ([]string, map[string]*workspaceEmbeddedClassSegment, string) {
	indexed := embeddedReferenceRangesFor(analysis)
	byName := make(map[string][]lsp.Range, len(indexed.cssClasses)+len(indexed.htmlClasses)+len(indexed.javascriptClasses))
	for name, ranges := range indexed.cssClasses {
		byName[name] = append(byName[name], ranges...)
	}
	for name, ranges := range indexed.htmlClasses {
		byName[name] = append(byName[name], ranges...)
	}
	for name, ranges := range indexed.javascriptClasses {
		byName[name] = append(byName[name], ranges...)
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	hash := sha256.New()
	segments := make(map[string]*workspaceEmbeddedClassSegment, len(names))
	for _, name := range names {
		fingerprint := workspaceEmbeddedClassFingerprint(name, byName[name])
		segments[name] = &workspaceEmbeddedClassSegment{documentKey: key, parsed: parsed, fingerprint: fingerprint, ranges: byName[name]}
		_, _ = hash.Write([]byte(name))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(fingerprint))
	}
	return names, segments, hex.EncodeToString(hash.Sum(nil))
}

func workspaceEmbeddedClassFingerprint(name string, ranges []lsp.Range) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(name))
	var number [8]byte
	for _, item := range ranges {
		binary.LittleEndian.PutUint32(number[:4], uint32(item.Start.Line))
		binary.LittleEndian.PutUint32(number[4:], uint32(item.Start.Character))
		_, _ = hash.Write(number[:])
		binary.LittleEndian.PutUint32(number[:4], uint32(item.End.Line))
		binary.LittleEndian.PutUint32(number[4:], uint32(item.End.Character))
		_, _ = hash.Write(number[:])
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func workspaceReferenceSegmentFingerprint(base string, counts workspaceReferenceCountSummary, declarationRanges map[lsp.Range]struct{}) string {
	fingerprint, _ := workspaceReferenceSegmentFingerprints(base, "", counts, declarationRanges)
	return fingerprint
}

// workspaceReferenceSegmentFingerprints hashes the count and location bases
// against one shared encoding of counts and sorted declaration ranges.
func workspaceReferenceSegmentFingerprints(countBase, locationBase string, counts workspaceReferenceCountSummary, declarationRanges map[lsp.Range]struct{}) (string, string) {
	values := [...]int{
		counts.Declarations, counts.Reads, counts.Writes, counts.Calls, counts.Crefs,
		counts.ObjectInitializations, counts.CodeLensReferences, counts.CodeLensCalls,
		counts.UnqualifiedCodeLensReferences, counts.UnqualifiedCodeLensCalls, counts.Total,
	}
	var storage [256]byte
	suffix := storage[:0]
	for _, value := range values {
		suffix = binary.LittleEndian.AppendUint64(suffix, uint64(value))
	}
	if len(declarationRanges) > 0 {
		ranges := make([]lsp.Range, 0, len(declarationRanges))
		for item := range declarationRanges {
			ranges = append(ranges, item)
		}
		sort.Slice(ranges, func(i, j int) bool {
			if ranges[i].Start.Line != ranges[j].Start.Line {
				return ranges[i].Start.Line < ranges[j].Start.Line
			}
			if ranges[i].Start.Character != ranges[j].Start.Character {
				return ranges[i].Start.Character < ranges[j].Start.Character
			}
			if ranges[i].End.Line != ranges[j].End.Line {
				return ranges[i].End.Line < ranges[j].End.Line
			}
			return ranges[i].End.Character < ranges[j].End.Character
		})
		for _, item := range ranges {
			suffix = binary.LittleEndian.AppendUint32(suffix, uint32(item.Start.Line))
			suffix = binary.LittleEndian.AppendUint32(suffix, uint32(item.Start.Character))
			suffix = binary.LittleEndian.AppendUint32(suffix, uint32(item.End.Line))
			suffix = binary.LittleEndian.AppendUint32(suffix, uint32(item.End.Character))
		}
	}
	hash := sha256.New()
	fingerprint := func(base string) string {
		hash.Reset()
		_, _ = io.WriteString(hash, base)
		_, _ = hash.Write(suffix)
		var sum [sha256.Size]byte
		return hex.EncodeToString(hash.Sum(sum[:0]))
	}
	return fingerprint(countBase), fingerprint(locationBase)
}

func (index *workspaceReferenceIndex) applyPrepared(prepared []workspaceReferencePreparedDocument) workspaceReferenceIndexUpdate {
	countChangedNames := map[string]struct{}{}
	locationChangedNames := map[string]struct{}{}
	embeddedChangedNames := map[string]struct{}{}
	update := workspaceReferenceIndexUpdate{}
	index.mu.Lock()
	defer index.mu.Unlock()
	for _, next := range prepared {
		if next.parsed == nil {
			continue
		}
		if next.ticket <= index.clearedThrough.Load() {
			continue
		}
		if next.ticket <= index.deletedThrough[next.key] {
			continue
		}
		current, found := index.documents[next.key]
		if found && current.ticket > next.ticket {
			continue
		}
		if next.unchangedSource {
			if !found || current.sourceHash != next.sourceHash {
				continue
			}
			index.replaceSegmentDocuments(next.key, current, next.parsed, next.ticket)
			update.SemanticUnchangedDocuments++
			continue
		}
		documentID := current.documentID
		if !found {
			index.nextDocumentID++
			documentID = index.nextDocumentID
		}
		for _, segment := range next.segments {
			segment.documentID = documentID
		}
		if found && !current.countSummaryOnly && current.countFingerprint == next.countFingerprint && current.locationFingerprint == next.locationFingerprint && current.embeddedFingerprint == next.embeddedFingerprint {
			current.sourceHash = next.sourceHash
			index.replaceSegmentDocuments(next.key, current, next.parsed, next.ticket)
			update.SemanticUnchangedDocuments++
			continue
		}
		if found {
			delete(index.parsedDocuments, current.parsed)
			for _, name := range current.names {
				oldSegment := current.segments[name]
				newSegment := next.segments[name]
				if newSegment != nil && oldSegment.countFingerprint == newSegment.countFingerprint && oldSegment.locationFingerprint == newSegment.locationFingerprint {
					index.names[name][documentID] = newSegment
					continue
				}
				if newSegment == nil || oldSegment.countFingerprint != newSegment.countFingerprint {
					countChangedNames[name] = struct{}{}
				}
				if newSegment == nil || oldSegment.locationFingerprint != newSegment.locationFingerprint {
					locationChangedNames[name] = struct{}{}
				}
				delete(index.names[name], documentID)
				if len(index.names[name]) == 0 {
					delete(index.names, name)
				}
			}
		}
		for name, segment := range next.segments {
			if found {
				oldSegment := current.segments[name]
				if oldSegment != nil && oldSegment.countFingerprint == segment.countFingerprint && oldSegment.locationFingerprint == segment.locationFingerprint {
					continue
				}
				if oldSegment == nil {
					countChangedNames[name] = struct{}{}
					locationChangedNames[name] = struct{}{}
				}
			} else {
				countChangedNames[name] = struct{}{}
				locationChangedNames[name] = struct{}{}
			}
			if index.names[name] == nil {
				index.names[name] = map[uint64]*workspaceReferenceDocumentSegment{}
			}
			index.names[name][documentID] = segment
		}
		if found {
			for _, name := range current.embeddedNames {
				oldSegment := current.embeddedSegments[name]
				newSegment := next.embeddedSegments[name]
				if newSegment != nil && oldSegment.fingerprint == newSegment.fingerprint {
					index.embeddedClasses[name][next.key] = newSegment
					continue
				}
				delete(index.embeddedClasses[name], next.key)
				if len(index.embeddedClasses[name]) == 0 {
					delete(index.embeddedClasses, name)
				}
				embeddedChangedNames[name] = struct{}{}
			}
		}
		for name, segment := range next.embeddedSegments {
			if found {
				if oldSegment := current.embeddedSegments[name]; oldSegment != nil && oldSegment.fingerprint == segment.fingerprint {
					continue
				}
			}
			if index.embeddedClasses[name] == nil {
				index.embeddedClasses[name] = map[string]*workspaceEmbeddedClassSegment{}
			}
			index.embeddedClasses[name][next.key] = segment
			embeddedChangedNames[name] = struct{}{}
		}
		index.documents[next.key] = workspaceReferenceIndexEntry{
			documentID: documentID, parsed: next.parsed, sourceHash: next.sourceHash, countFingerprint: next.countFingerprint, locationFingerprint: next.locationFingerprint,
			embeddedFingerprint: next.embeddedFingerprint,
			names:               next.names, segments: next.segments, embeddedNames: next.embeddedNames, embeddedSegments: next.embeddedSegments,
			countSummaryOnly: next.countSummaryOnly, ticket: next.ticket,
		}
		delete(index.deletedThrough, next.key)
		index.parsedDocuments[next.parsed] = next.key
		update.ChangedDocuments++
	}
	update.CountAffectedNames = sortedWorkspaceReferenceNames(countChangedNames)
	update.LocationAffectedNames = sortedWorkspaceReferenceNames(locationChangedNames)
	update.EmbeddedAffectedNames = sortedWorkspaceReferenceNames(embeddedChangedNames)
	affectedNames := make(map[string]struct{}, len(countChangedNames)+len(locationChangedNames))
	for name := range countChangedNames {
		affectedNames[name] = struct{}{}
	}
	for name := range locationChangedNames {
		affectedNames[name] = struct{}{}
	}
	update.AffectedNames = sortedWorkspaceReferenceNames(affectedNames)
	if len(update.CountAffectedNames) > 0 {
		index.countRevision.Add(1)
	}
	if len(update.LocationAffectedNames) > 0 {
		index.locationRevision.Add(1)
	}
	if len(update.EmbeddedAffectedNames) > 0 {
		index.embeddedRevision.Add(1)
	}
	return update
}

func (index *workspaceReferenceIndex) removeDocument(uri string) workspaceReferenceIndexUpdate {
	if index == nil || uri == "" {
		return workspaceReferenceIndexUpdate{}
	}
	key := workspacepkg.FileIdentityKeyFromURI(uri)
	ticket := index.sequence.Add(1)
	index.mu.Lock()
	defer index.mu.Unlock()
	if ticket <= index.deletedThrough[key] {
		return workspaceReferenceIndexUpdate{}
	}
	index.deletedThrough[key] = ticket
	current, found := index.documents[key]
	if !found {
		return workspaceReferenceIndexUpdate{}
	}
	delete(index.documents, key)
	delete(index.parsedDocuments, current.parsed)
	for _, name := range current.names {
		delete(index.names[name], current.documentID)
		if len(index.names[name]) == 0 {
			delete(index.names, name)
		}
	}
	for _, name := range current.embeddedNames {
		delete(index.embeddedClasses[name], key)
		if len(index.embeddedClasses[name]) == 0 {
			delete(index.embeddedClasses, name)
		}
	}
	names := append([]string(nil), current.names...)
	embeddedNames := append([]string(nil), current.embeddedNames...)
	if len(names) > 0 {
		index.countRevision.Add(1)
		index.locationRevision.Add(1)
	}
	if len(embeddedNames) > 0 {
		index.embeddedRevision.Add(1)
	}
	return workspaceReferenceIndexUpdate{
		ChangedDocuments:      1,
		CountAffectedNames:    names,
		LocationAffectedNames: append([]string(nil), names...),
		EmbeddedAffectedNames: embeddedNames,
		AffectedNames:         append([]string(nil), names...),
	}
}

func sortedWorkspaceReferenceNames(names map[string]struct{}) []string {
	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func (index *workspaceReferenceIndex) replaceSegmentDocuments(key string, current workspaceReferenceIndexEntry, parsed *core.ParsedDocument, ticket uint64) {
	delete(index.parsedDocuments, current.parsed)
	segments := make(map[string]*workspaceReferenceDocumentSegment, len(current.segments))
	for name, old := range current.segments {
		next := *old
		next.parsed = parsed
		segments[name] = &next
		index.names[name][current.documentID] = &next
	}
	embeddedSegments := make(map[string]*workspaceEmbeddedClassSegment, len(current.embeddedSegments))
	for name, old := range current.embeddedSegments {
		next := *old
		next.parsed = parsed
		embeddedSegments[name] = &next
		index.embeddedClasses[name][key] = &next
	}
	current.parsed = parsed
	current.segments = segments
	current.embeddedSegments = embeddedSegments
	current.ticket = ticket
	index.documents[key] = current
	index.parsedDocuments[parsed] = key
}

func summarizeWorkspaceReferencePostings(postings []vbscript.ReferencePosting, declarationRanges map[lsp.Range]struct{}) workspaceReferenceCountSummary {
	return summarizeWorkspaceReferencePostingsWithResolutions(
		postings,
		vbscript.GlobalReferenceResolutions(postings),
		declarationRanges,
	)
}

func summarizeWorkspaceReferencePostingsWithResolutions(
	postings []vbscript.ReferencePosting,
	globalResolutions []bool,
	declarationRanges map[lsp.Range]struct{},
) workspaceReferenceCountSummary {
	var result workspaceReferenceCountSummary
	result.Total = len(postings)
	for index, posting := range postings {
		isDeclaration := posting.HasRole(vbscript.ReferenceRoleDeclaration)
		if !isDeclaration {
			_, isDeclaration = declarationRanges[posting.Range]
		}
		if isDeclaration {
			result.Declarations++
		}
		if posting.HasRole(vbscript.ReferenceRoleRead) {
			result.Reads++
		}
		if posting.HasRole(vbscript.ReferenceRoleWrite) {
			result.Writes++
		}
		if posting.HasRole(vbscript.ReferenceRoleCall) {
			result.Calls++
		}
		if posting.HasRole(vbscript.ReferenceRoleCref) {
			result.Crefs++
		}
		if posting.HasRole(vbscript.ReferenceRoleObjectInitialization) {
			result.ObjectInitializations++
		}
		if isDeclaration || posting.HasRole(vbscript.ReferenceRoleCref) || posting.HasRole(vbscript.ReferenceRoleObjectInitialization) {
			continue
		}
		result.CodeLensReferences++
		if posting.HasRole(vbscript.ReferenceRoleCall) {
			result.CodeLensCalls++
		}
		if globalResolutions[index] {
			result.UnqualifiedCodeLensReferences++
			if posting.HasRole(vbscript.ReferenceRoleCall) {
				result.UnqualifiedCodeLensCalls++
			}
		}
	}
	return result
}
