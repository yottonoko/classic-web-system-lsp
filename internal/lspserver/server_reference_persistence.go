package lspserver

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func (s *Server) restoreWorkspaceReferenceBatch(doc *core.TextDocument, parsed *core.ParsedDocument) bool {
	if doc == nil || parsed == nil {
		return false
	}
	s.mu.Lock()
	generation := s.referenceGeneration
	s.mu.Unlock()
	declarations := s.workspaceReferenceCodeLensPlan(parsed).declarations
	documents := s.workspaceReferenceDocuments(parsed)
	s.restoreWorkspaceReferenceCountSummaries(documents)
	descriptors := s.workspaceReferenceQueryDescriptors(parsed, declarations, documents)
	return s.restoreWorkspaceReferenceQueriesWithDescriptors(parsed, descriptors, generation) > 0
}

func (s *Server) persistWorkspaceReferenceBatch(uri string, generation uint64) {
	_, parsed := s.parsed(uri)
	if parsed == nil {
		return
	}
	declarations := s.workspaceReferenceCodeLensPlan(parsed).declarations
	documents := s.workspaceReferenceDocuments(parsed)
	s.persistWorkspaceReferenceBatchWithDescriptors(uri, generation, s.workspaceReferenceQueryDescriptors(parsed, declarations, documents))
}

func (s *Server) persistWorkspaceReferenceBatchWithDescriptors(uri string, generation uint64, descriptors []workspaceReferenceQueryDescriptor) {
	if len(descriptors) == 0 {
		return
	}
	writes := make([]workspacepkg.DiskReferenceQueryWrite, 0, len(descriptors))
	s.mu.Lock()
	if s.referenceGeneration != generation {
		s.mu.Unlock()
		return
	}
	for _, descriptor := range descriptors {
		declaration := descriptor.declaration
		key := workspaceReferenceRequestKey(uri, declaration.Range.Start, false, declaration.Kind, generation, declaration.Name)
		count, ok := s.referenceCounts[key]
		if !ok {
			continue
		}
		payload, err := cbor.Marshal(persistedWorkspaceReferenceQuery{
			SchemaVersion: workspaceReferenceQuerySchemaVersion, DeclarationFingerprint: descriptor.declarationFingerprint,
			NameFingerprint: descriptor.nameFingerprint, ScopeFingerprint: descriptor.scopeFingerprint, Count: count,
		})
		if err == nil {
			writes = append(writes, workspacepkg.DiskReferenceQueryWrite{Key: descriptor.key, Value: payload})
		}
	}
	s.mu.Unlock()
	if len(writes) == 0 {
		return
	}
	cache := s.diskCacheForUse()
	if cache == nil || !cache.Enabled() {
		return
	}
	if err := cache.QueueReferenceQueryWrites(writes); err != nil {
		s.logServerWarning("[asp-lsp] analysisDatabase.referenceQueries.write.failed: " + err.Error())
		return
	}
	s.logAnalysisDatabaseEvent("referenceQueries", "queued", map[string]any{"symbols": len(writes), "uri": uri})
}

func workspaceReferenceDocumentCacheKey(documentKey string) []byte {
	return []byte(workspacepkg.DiskContentHash("workspace-reference-document-v1\x00" + documentKey))
}

func workspaceReferencePostingCacheKey(documentKey, name string) string {
	return workspacepkg.DiskContentHash("workspace-reference-posting-v1\x00" + documentKey + "\x00" + strings.ToLower(name))
}

var workspaceReferenceCountSummaryPreparationTestHook struct {
	sync.Mutex
	fn func(string)
}

var workspaceReferenceCountSummaryPublicationTestHook struct {
	sync.Mutex
	fn func(string)
}

func runWorkspaceReferenceCountSummaryPreparationTestHook(documentKey string) {
	workspaceReferenceCountSummaryPreparationTestHook.Lock()
	hook := workspaceReferenceCountSummaryPreparationTestHook.fn
	workspaceReferenceCountSummaryPreparationTestHook.Unlock()
	if hook != nil {
		hook(documentKey)
	}
}

func runWorkspaceReferenceCountSummaryPublicationTestHook(documentKey string) {
	workspaceReferenceCountSummaryPublicationTestHook.Lock()
	hook := workspaceReferenceCountSummaryPublicationTestHook.fn
	workspaceReferenceCountSummaryPublicationTestHook.Unlock()
	if hook != nil {
		hook(documentKey)
	}
}

type workspaceReferenceCountSummaryPrepared struct {
	document       *core.ParsedDocument
	sourceHash     string
	countSummaries map[string]persistedWorkspaceReferenceCountSummary
}

type workspaceReferenceCountSummaryPending struct {
	parsed *core.ParsedDocument
	key    []byte
	ticket uint64
}

func prepareWorkspaceReferenceCountSummary(document *core.ParsedDocument, documentKey string, payload []byte) (workspaceReferenceCountSummaryPrepared, bool) {
	if document == nil || len(payload) == 0 {
		return workspaceReferenceCountSummaryPrepared{}, false
	}
	runWorkspaceReferenceCountSummaryPreparationTestHook(documentKey)
	var manifest persistedWorkspaceReferenceDocument
	if cbor.Unmarshal(payload, &manifest) != nil || manifest.SchemaVersion != workspaceReferenceDocumentSchemaVersion || manifest.DocumentKey != documentKey {
		return workspaceReferenceCountSummaryPrepared{}, false
	}
	sourceHash := workspacepkg.DiskContentHash(document.Text)
	if manifest.SourceHash != sourceHash || len(manifest.CountSummaries) == 0 {
		return workspaceReferenceCountSummaryPrepared{}, false
	}
	return workspaceReferenceCountSummaryPrepared{
		document: document, sourceHash: sourceHash, countSummaries: manifest.CountSummaries,
	}, true
}

func (s *Server) restoreWorkspaceReferenceCountSummaries(documents []*core.ParsedDocument) int {
	s.mu.Lock()
	generation := s.referenceGeneration
	s.mu.Unlock()
	return s.restoreWorkspaceReferenceCountSummariesContext(context.Background(), documents, generation)
}

func (s *Server) restoreWorkspaceReferenceCountSummariesContext(ctx context.Context, documents []*core.ParsedDocument, generation uint64) int {
	if ctx == nil {
		ctx = context.Background()
	}
	cache := s.diskCacheForUse()
	if cache == nil || !cache.Enabled() || len(documents) == 0 || ctx.Err() != nil {
		return 0
	}
	pending := make([]workspaceReferenceCountSummaryPending, 0, len(documents))
	ticket := s.referenceWorkspaceIndex.sequence.Add(1)
	s.mu.Lock()
	if s.referenceGeneration != generation {
		s.mu.Unlock()
		return 0
	}
	for _, document := range documents {
		if document == nil {
			continue
		}
		if _, restored := s.referenceCountSummariesRestored[document]; restored {
			continue
		}
		// Parsed documents are immutable revision snapshots, so reserving the
		// attempt also negative-caches missing or stale disk entries safely.
		s.referenceCountSummariesRestored[document] = struct{}{}
		pending = append(pending, workspaceReferenceCountSummaryPending{
			parsed: document,
			key:    workspaceReferenceDocumentCacheKey(workspacepkg.FileIdentityKeyFromURI(document.URI)),
			ticket: ticket,
		})
	}
	s.mu.Unlock()
	if len(pending) == 0 {
		return 0
	}
	manifestKeys := make([][]byte, len(pending))
	for index := range pending {
		manifestKeys[index] = pending[index].key
	}
	manifestValues := cache.ReadReferenceValuesAligned(workspacepkg.DiskReferenceDocuments, manifestKeys)
	prepared := make([]workspaceReferenceCountSummaryPrepared, len(pending))
	valid := make([]bool, len(pending))
	valueCount := len(manifestValues)
	if valueCount > len(pending) {
		valueCount = len(pending)
	}
	s.analysisWorkers.parallelForBulk(ctx, valueCount, func(workerCtx context.Context, index int) {
		if workerCtx.Err() != nil {
			return
		}
		document := pending[index].parsed
		documentKey := workspacepkg.FileIdentityKeyFromURI(document.URI)
		prepared[index], valid[index] = prepareWorkspaceReferenceCountSummary(document, documentKey, manifestValues[index])
	})
	if ctx.Err() != nil || !s.referenceGenerationCurrent(generation) {
		s.releaseWorkspaceReferenceCountSummaryReservations(pending)
		return 0
	}
	restored := 0
	for index, item := range prepared {
		if !valid[index] {
			continue
		}
		if ctx.Err() != nil {
			s.releaseWorkspaceReferenceCountSummaryReservations(pending[index:])
			break
		}
		s.mu.Lock()
		if s.referenceGeneration != generation {
			s.mu.Unlock()
			s.releaseWorkspaceReferenceCountSummaryReservations(pending[index:])
			break
		}
		runWorkspaceReferenceCountSummaryPublicationTestHook(workspacepkg.FileIdentityKeyFromURI(item.document.URI))
		update := s.referenceWorkspaceIndex.seedPersistedCountSummaryWithTicket(item.document, item.sourceHash, item.countSummaries, pending[index].ticket)
		s.mu.Unlock()
		if update.ChangedDocuments > 0 {
			restored++
		}
	}
	if restored > 0 {
		s.logAnalysisDatabaseEvent("referenceCountSummaries", "restore", map[string]any{"documents": restored, "requested": len(pending)})
	}
	return restored
}

func (s *Server) referenceGenerationCurrent(generation uint64) bool {
	s.mu.Lock()
	current := s.referenceGeneration == generation
	s.mu.Unlock()
	return current
}

func (s *Server) releaseWorkspaceReferenceCountSummaryReservations(pending []workspaceReferenceCountSummaryPending) {
	s.mu.Lock()
	for _, item := range pending {
		delete(s.referenceCountSummariesRestored, item.parsed)
	}
	s.mu.Unlock()
}

func (s *Server) restoreWorkspaceReferenceShards(documents []*core.ParsedDocument) {
	cache := s.diskCacheForUse()
	if cache == nil || !cache.Enabled() || len(documents) == 0 {
		return
	}
	manifestKeys := make([][]byte, len(documents))
	for index, document := range documents {
		if document != nil {
			manifestKeys[index] = workspaceReferenceDocumentCacheKey(workspacepkg.FileIdentityKeyFromURI(document.URI))
		}
	}
	manifestValues := cache.ReadReferenceValuesAligned(workspacepkg.DiskReferenceDocuments, manifestKeys)
	type pendingPosting struct {
		documentIndex int
		name          string
	}
	manifests := make([]persistedWorkspaceReferenceDocument, len(documents))
	postingKeys := make([][]byte, 0)
	pending := make([]pendingPosting, 0)
	for index, payload := range manifestValues {
		document := documents[index]
		if document == nil || len(payload) == 0 {
			continue
		}
		var manifest persistedWorkspaceReferenceDocument
		documentKey := workspacepkg.FileIdentityKeyFromURI(document.URI)
		if cbor.Unmarshal(payload, &manifest) != nil || manifest.SchemaVersion != workspaceReferenceDocumentSchemaVersion ||
			manifest.DocumentKey != documentKey || manifest.SourceHash != workspacepkg.DiskContentHash(document.Text) {
			continue
		}
		manifests[index] = manifest
		for _, name := range manifest.PostingKeys {
			postingKeys = append(postingKeys, []byte(workspaceReferencePostingCacheKey(documentKey, name)))
			pending = append(pending, pendingPosting{documentIndex: index, name: name})
		}
	}
	postingValues := cache.ReadReferenceValuesAligned(workspacepkg.DiskReferencePostings, postingKeys)
	postingsByDocument := make([]map[string][]vbscript.ReferencePosting, len(documents))
	valid := make([]bool, len(documents))
	for index, manifest := range manifests {
		if manifest.SchemaVersion != 0 {
			postingsByDocument[index] = make(map[string][]vbscript.ReferencePosting, len(manifest.PostingKeys))
			valid[index] = true
		}
	}
	for index, item := range pending {
		var posting persistedWorkspaceReferencePosting
		if index >= len(postingValues) || cbor.Unmarshal(postingValues[index], &posting) != nil ||
			posting.SchemaVersion != workspaceReferenceDocumentSchemaVersion || !strings.EqualFold(posting.Name, item.name) {
			valid[item.documentIndex] = false
			continue
		}
		postingsByDocument[item.documentIndex][strings.ToLower(item.name)] = posting.Postings
	}
	restored := 0
	for index, document := range documents {
		manifest := manifests[index]
		if document == nil || !valid[index] || len(postingsByDocument[index]) != len(manifest.PostingKeys) {
			continue
		}
		if _, cached := vbscript.CachedReferenceShard(document); cached {
			continue
		}
		vbscript.SeedReferenceShard(document, &vbscript.ReferenceShard{
			Version: vbscript.ReferenceShardVersion, Declarations: manifest.Declarations,
			Postings: postingsByDocument[index], Scopes: manifest.Scopes,
		})
		restored++
	}
	if restored > 0 {
		s.logAnalysisDatabaseEvent("referenceDocuments", "restore", map[string]any{"documents": restored, "requested": len(documents)})
	}
}

// hydrateWorkspaceReferenceSegments reads posting blobs only for a location
// request. Count-only CodeLens batches stay on the compact manifest summaries.
func (s *Server) hydrateWorkspaceReferenceSegments(segments []*workspaceReferenceDocumentSegment) []*workspaceReferenceDocumentSegment {
	if len(segments) == 0 {
		return segments
	}
	result := append([]*workspaceReferenceDocumentSegment(nil), segments...)
	pendingIndexes := make([]int, 0, len(segments))
	keys := make([][]byte, 0, len(segments))
	for index, segment := range segments {
		if segment == nil || segment.postingsLoaded {
			continue
		}
		if hydration := segment.hydration; hydration != nil {
			hydration.mu.Lock()
			if hydration.ready {
				hydrated := *segment
				hydrated.postings = hydration.postings
				hydrated.globalResolutions = hydration.globalResolutions
				hydrated.postingsLoaded = true
				hydrated.declarationRanges = hydration.declarationRanges
				result[index] = &hydrated
				hydration.mu.Unlock()
				continue
			}
			hydration.mu.Unlock()
		}
		pendingIndexes = append(pendingIndexes, index)
		keys = append(keys, []byte(workspaceReferencePostingCacheKey(segment.documentKey, segment.name)))
	}
	cache := s.diskCacheForUse()
	var values [][]byte
	if cache != nil && cache.Enabled() && len(keys) > 0 {
		values = cache.ReadReferenceValuesAligned(workspacepkg.DiskReferencePostings, keys)
	}
	for pendingIndex, segmentIndex := range pendingIndexes {
		segment := segments[segmentIndex]
		var postings []vbscript.ReferencePosting
		var globalResolutions []bool
		if pendingIndex < len(values) {
			var persisted persistedWorkspaceReferencePosting
			if cbor.Unmarshal(values[pendingIndex], &persisted) == nil && persisted.SchemaVersion == workspaceReferenceDocumentSchemaVersion && strings.EqualFold(persisted.Name, segment.name) {
				postings = persisted.Postings
				globalResolutions = vbscript.GlobalReferenceResolutions(postings)
				if summarizeWorkspaceReferencePostingsWithResolutions(postings, globalResolutions, segment.declarationRanges) != segment.counts {
					postings = nil
					globalResolutions = nil
				}
			}
		}
		declarationRanges := segment.declarationRanges
		if postings == nil {
			shard := vbscript.BuildReferenceShard(segment.parsed)
			postings = shard.PostingsFor(segment.name)
			globalResolutions = shard.GlobalResolutionsFor(segment.name)
			declarationRanges = vbReferenceDocumentIndexFor(segment.parsed).declarationRanges[segment.name]
		}
		if hydration := segment.hydration; hydration != nil {
			hydration.mu.Lock()
			if !hydration.ready {
				hydration.postings = postings
				hydration.globalResolutions = globalResolutions
				hydration.declarationRanges = declarationRanges
				hydration.ready = true
			} else {
				postings = hydration.postings
				globalResolutions = hydration.globalResolutions
				declarationRanges = hydration.declarationRanges
			}
			hydration.mu.Unlock()
		}
		hydrated := *segment
		hydrated.postings = postings
		hydrated.globalResolutions = globalResolutions
		hydrated.postingsLoaded = true
		hydrated.declarationRanges = declarationRanges
		result[segmentIndex] = &hydrated
	}
	return result
}

func (s *Server) persistWorkspaceReferenceDocument(manifest *workspaceDocumentArtifactManifest, shard *vbscript.ReferenceShard, revision uint64) {
	cache := s.diskCacheForUse()
	if cache == nil || !cache.Enabled() || manifest == nil || manifest.CST == nil {
		return
	}
	if !s.workspaceReferenceDocumentRevisionCurrent(manifest, revision) {
		return
	}
	if shard == nil {
		shard = vbscript.BuildReferenceShard(manifest.CST)
	}
	documentKey := workspacepkg.FileIdentityKeyFromURI(manifest.URI)
	manifestKey := workspaceReferenceDocumentCacheKey(documentKey)
	names := make([]string, 0, len(shard.Postings))
	for name := range shard.Postings {
		names = append(names, name)
	}
	sort.Strings(names)
	postingFingerprints := make(map[string]string, len(names))
	for _, name := range names {
		summary, ok := shard.SummaryFor(name)
		if !ok {
			return
		}
		postingFingerprints[name] = summary.LocationFingerprint
	}
	persistedManifest := persistedWorkspaceReferenceDocument{
		SchemaVersion: workspaceReferenceDocumentSchemaVersion, DocumentKey: documentKey,
		SourceHash: workspacepkg.DiskContentHash(manifest.CST.Text), Declarations: shard.Declarations,
		Scopes: shard.Scopes, PostingKeys: names, PostingFingerprints: postingFingerprints,
		CountSummaries: s.workspaceReferencePersistedCountSummaries(manifest.CST, shard),
	}
	manifestPayload, err := cbor.Marshal(persistedManifest)
	if err != nil {
		return
	}
	oldManifest, _ := workspaceReferencePersistedManifest(cache, manifestKey)
	oldPostingKeys, currentPostingKeys, changedNames := workspaceReferencePostingDelta(documentKey, oldManifest, names, postingFingerprints)
	changedNames = workspaceReferenceRepairMissingPostings(
		names,
		changedNames,
		cache.ReferenceValuesPresentAligned(workspacepkg.DiskReferencePostings, currentPostingKeys),
	)
	newPostings := make(map[string][]byte, len(changedNames))
	for _, name := range changedNames {
		payload, err := cbor.Marshal(persistedWorkspaceReferencePosting{
			SchemaVersion: workspaceReferenceDocumentSchemaVersion, Name: name, Postings: shard.Postings[name],
		})
		if err != nil {
			return
		}
		newPostings[workspaceReferencePostingCacheKey(documentKey, name)] = payload
	}
	s.mu.Lock()
	if !s.workspaceReferenceDocumentRevisionCurrentLocked(manifest, revision) {
		s.mu.Unlock()
		return
	}
	if hook := s.workspaceReferencePersistenceBeforeQueueTestHook; hook != nil {
		hook()
	}
	err = cache.QueueReferenceDocumentReplacements([]workspacepkg.DiskReferenceDocumentReplacement{{
		DocumentKey: manifestKey, DocumentValue: manifestPayload, OldPostingKeys: oldPostingKeys,
		NewPostings: newPostings, CurrentPostingKeys: currentPostingKeys,
	}})
	s.mu.Unlock()
	if err != nil {
		s.logServerWarning("[asp-lsp] analysisDatabase.referenceDocuments.write.failed: " + err.Error())
		return
	}
	s.logAnalysisDatabaseEvent("referenceDocuments", "queued", map[string]any{"documents": 1, "uri": manifest.URI})
}

func (s *Server) workspaceReferencePersistedCountSummaries(parsed *core.ParsedDocument, shard *vbscript.ReferenceShard) map[string]persistedWorkspaceReferenceCountSummary {
	if parsed == nil || shard == nil {
		return nil
	}
	facts := vbReferenceDocumentIndexFor(parsed)
	result := make(map[string]persistedWorkspaceReferenceCountSummary, len(shard.Postings))
	for _, name := range shard.NormalizedNames() {
		postings := shard.Postings[name]
		declarationRanges := facts.declarationRanges[name]
		counts := summarizeWorkspaceReferencePostings(postings, declarationRanges)
		base, _ := shard.SummaryFor(name)
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
		result[name] = persistedWorkspaceReferenceCountSummary{
			CountFingerprint:    workspaceReferenceSegmentFingerprint(base.CountFingerprint, counts, declarationRanges),
			LocationFingerprint: workspaceReferenceSegmentFingerprint(base.LocationFingerprint, counts, declarationRanges),
			Counts:              counts, DeclarationRanges: ranges,
		}
	}
	for _, declaration := range s.workspaceReferenceCodeLensPlan(parsed).declarations {
		if !declaration.Implicit {
			continue
		}
		name := strings.ToLower(declaration.Name)
		stored, ok := result[name]
		if !ok {
			continue
		}
		for _, posting := range shard.Postings[name] {
			_, excluded := facts.declarationRanges[name][posting.Range]
			if posting.Range != declaration.Range || excluded || posting.HasRole(vbscript.ReferenceRoleDeclaration) || posting.HasRole(vbscript.ReferenceRoleCref) || posting.HasRole(vbscript.ReferenceRoleObjectInitialization) {
				continue
			}
			adjustment := persistedWorkspaceReferenceCountAdjustment{
				Range: declaration.Range, CodeLensReferences: 1, UnqualifiedCodeLensReferences: 1,
			}
			if posting.HasRole(vbscript.ReferenceRoleCall) {
				adjustment.CodeLensCalls = 1
				adjustment.UnqualifiedCodeLensCalls = 1
			}
			stored.ImplicitAdjustments = append(stored.ImplicitAdjustments, adjustment)
			result[name] = stored
			break
		}
	}
	return result
}

func (s *Server) workspaceReferenceDocumentRevisionCurrent(manifest *workspaceDocumentArtifactManifest, revision uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.workspaceReferenceDocumentRevisionCurrentLocked(manifest, revision)
}

func (s *Server) workspaceReferenceDocumentRevisionCurrentLocked(manifest *workspaceDocumentArtifactManifest, revision uint64) bool {
	return s.workspaceArtifacts[manifest.DocumentID] == manifest &&
		s.workspaceArtifactRevisions[manifest.DocumentID] == revision &&
		s.workspaceReferenceManifestMatchesBackingLocked(manifest)
}

func workspaceReferenceOldPostingKeys(cache *workspacepkg.DiskAnalysisCache, manifestKey []byte) [][]byte {
	oldManifest, ok := workspaceReferencePersistedManifest(cache, manifestKey)
	if !ok {
		return nil
	}
	keys := make([][]byte, 0, len(oldManifest.PostingKeys))
	for _, name := range oldManifest.PostingKeys {
		keys = append(keys, []byte(workspaceReferencePostingCacheKey(oldManifest.DocumentKey, name)))
	}
	return keys
}

func workspaceReferencePersistedManifest(cache *workspacepkg.DiskAnalysisCache, manifestKey []byte) (persistedWorkspaceReferenceDocument, bool) {
	if cache == nil || len(manifestKey) == 0 {
		return persistedWorkspaceReferenceDocument{}, false
	}
	payload := cache.ReadReferenceValuesAligned(workspacepkg.DiskReferenceDocuments, [][]byte{manifestKey})[0]
	var manifest persistedWorkspaceReferenceDocument
	if cbor.Unmarshal(payload, &manifest) != nil || manifest.DocumentKey == "" {
		return persistedWorkspaceReferenceDocument{}, false
	}
	return manifest, true
}

func workspaceReferencePostingDelta(
	documentKey string,
	oldManifest persistedWorkspaceReferenceDocument,
	names []string,
	postingFingerprints map[string]string,
) (oldPostingKeys, currentPostingKeys [][]byte, changedNames []string) {
	oldNames := make(map[string]struct{}, len(oldManifest.PostingKeys))
	if oldManifest.DocumentKey == documentKey {
		for _, name := range oldManifest.PostingKeys {
			oldNames[strings.ToLower(name)] = struct{}{}
		}
	}
	currentNames := make(map[string]struct{}, len(names))
	currentPostingKeys = make([][]byte, 0, len(names))
	changedNames = make([]string, 0, len(names))
	for _, name := range names {
		name = strings.ToLower(name)
		currentNames[name] = struct{}{}
		postingKey := workspaceReferencePostingCacheKey(documentKey, name)
		currentPostingKeys = append(currentPostingKeys, []byte(postingKey))
		_, previouslyOwned := oldNames[name]
		if !previouslyOwned || oldManifest.PostingFingerprints[name] != postingFingerprints[name] {
			changedNames = append(changedNames, name)
		}
	}
	if oldManifest.DocumentKey != documentKey {
		return nil, currentPostingKeys, changedNames
	}
	for _, name := range oldManifest.PostingKeys {
		if _, retained := currentNames[strings.ToLower(name)]; retained {
			continue
		}
		oldPostingKeys = append(oldPostingKeys, []byte(workspaceReferencePostingCacheKey(documentKey, name)))
	}
	return oldPostingKeys, currentPostingKeys, changedNames
}

func workspaceReferenceRepairMissingPostings(names, changedNames []string, present []bool) []string {
	changed := make(map[string]struct{}, len(changedNames))
	for _, name := range changedNames {
		changed[strings.ToLower(name)] = struct{}{}
	}
	for index, name := range names {
		name = strings.ToLower(name)
		if index < len(present) && present[index] {
			continue
		}
		if _, ok := changed[name]; ok {
			continue
		}
		changed[name] = struct{}{}
		changedNames = append(changedNames, name)
	}
	return changedNames
}

func (s *Server) queueWorkspaceReferenceDocumentTombstoneIfCurrent(uri string, documentID workspaceDocumentID, revision uint64) {
	cache := s.diskCacheForUse()
	if cache == nil || !cache.Enabled() || uri == "" {
		return
	}
	documentKey := workspacepkg.FileIdentityKeyFromURI(uri)
	manifestKey := workspaceReferenceDocumentCacheKey(documentKey)
	oldPostingKeys := workspaceReferenceOldPostingKeys(cache, manifestKey)
	if hook := s.workspaceReferenceTombstoneBeforeQueueTestHook; hook != nil {
		hook()
	}
	s.mu.Lock()
	if s.workspaceArtifacts[documentID] != nil || s.workspaceArtifactRevisions[documentID] != revision {
		s.mu.Unlock()
		return
	}
	err := cache.QueueReferenceDocumentReplacements([]workspacepkg.DiskReferenceDocumentReplacement{{
		DocumentKey: manifestKey, OldPostingKeys: oldPostingKeys, CurrentPostingKeys: make([][]byte, 0),
	}})
	s.mu.Unlock()
	if err != nil {
		s.logServerWarning("[asp-lsp] analysisDatabase.referenceDocuments.delete.failed: " + err.Error())
	}
}

func (s *Server) restoreWorkspaceReferenceQueriesWithDescriptors(parsed *core.ParsedDocument, descriptors []workspaceReferenceQueryDescriptor, generation uint64) int {
	cache := s.diskCacheForUse()
	if cache == nil || !cache.Enabled() || parsed == nil || len(descriptors) == 0 {
		return 0
	}
	keys := make([][]byte, len(descriptors))
	for index := range descriptors {
		keys[index] = descriptors[index].key
	}
	values := cache.ReadReferenceValuesAligned(workspacepkg.DiskReferenceQueries, keys)
	invalidWrites := make([]workspacepkg.DiskReferenceQueryWrite, 0)
	restored := 0
	for index, payload := range values {
		if len(payload) == 0 || index >= len(descriptors) {
			continue
		}
		descriptor := descriptors[index]
		var persisted persistedWorkspaceReferenceQuery
		if cbor.Unmarshal(payload, &persisted) != nil || persisted.SchemaVersion != workspaceReferenceQuerySchemaVersion ||
			persisted.DeclarationFingerprint != descriptor.declarationFingerprint || persisted.NameFingerprint != descriptor.nameFingerprint ||
			persisted.ScopeFingerprint != descriptor.scopeFingerprint || persisted.Count < 0 {
			invalidWrites = append(invalidWrites, workspacepkg.DiskReferenceQueryWrite{Key: descriptor.key})
			continue
		}
		declaration := descriptor.declaration
		requestKey := workspaceReferenceRequestKey(parsed.URI, declaration.Range.Start, false, declaration.Kind, generation, declaration.Name)
		inserted := false
		s.mu.Lock()
		if s.referenceGeneration == generation {
			if _, exists := s.referenceCounts[requestKey]; !exists {
				s.storeWorkspaceReferenceCountLocked(requestKey, persisted.Count)
				restored++
				inserted = true
			}
		}
		s.mu.Unlock()
		if inserted {
			s.rememberWorkspaceReferenceCount(parsed, declaration, persisted.Count)
		}
	}
	if len(invalidWrites) > 0 {
		_ = cache.QueueReferenceQueryWrites(invalidWrites)
	}
	s.logAnalysisDatabaseEvent("referenceQueries", "read", map[string]any{"entries": len(descriptors), "hits": restored, "misses": len(descriptors) - restored, "uri": parsed.URI})
	return restored
}

func (s *Server) workspaceReferenceQueryDescriptors(parsed *core.ParsedDocument, declarations []vbUsageDeclaration, documents []*core.ParsedDocument) []workspaceReferenceQueryDescriptor {
	return s.workspaceReferenceQueryDescriptorsContext(context.Background(), parsed, declarations, documents)
}

func (s *Server) workspaceReferenceQueryDescriptorsContext(ctx context.Context, parsed *core.ParsedDocument, declarations []vbUsageDeclaration, documents []*core.ParsedDocument) []workspaceReferenceQueryDescriptor {
	if ctx.Err() != nil {
		return nil
	}
	if parsed == nil || len(declarations) == 0 {
		return nil
	}
	nameFingerprints, scopeFingerprint := s.workspaceReferenceDescriptorFingerprintsContext(ctx, parsed, declarations, documents)
	if ctx.Err() != nil {
		return nil
	}
	s.mu.Lock()
	settings := struct {
		Procedures     bool `json:"procedures"`
		Globals        bool `json:"globals"`
		Classes        bool `json:"classes"`
		ClassMembers   bool `json:"classMembers"`
		IncludeRelated bool `json:"includeRelated"`
	}{
		Procedures: s.settings.CodeLensReferenceProcedures, Globals: s.settings.CodeLensReferenceGlobals,
		Classes: s.settings.CodeLensReferenceClasses, ClassMembers: s.settings.CodeLensReferenceClassMembers,
		IncludeRelated: s.settings.CodeLensIncludeRelatedIncludeTrees,
	}
	s.mu.Unlock()
	encodedSettings, _ := json.Marshal(settings)
	settingsFingerprint := workspacepkg.DiskContentHash(s.workspaceDiskSettingsKey() + "\x00" + string(encodedSettings))
	origin := workspacepkg.FileIdentityKeyFromURI(parsed.URI)
	ordinals := make(map[string]int, len(declarations))
	descriptors := make([]workspaceReferenceQueryDescriptor, 0, len(declarations))
	for _, declaration := range declarations {
		if ctx.Err() != nil {
			return nil
		}
		base := strings.ToLower(declaration.Name) + "\x00" + declaration.Kind + "\x00" + strings.ToLower(declaration.MemberOf) + "\x00" + strings.ToLower(declaration.Scope) + "\x00" + strconv.FormatBool(declaration.Implicit)
		if declaration.Implicit {
			accepted, complete := s.implicitGlobalReferenceDocumentKeysContext(ctx, parsed, declaration)
			if !complete || ctx.Err() != nil {
				return nil
			}
			acceptedKeys := make([]string, 0, len(accepted))
			for key := range accepted {
				acceptedKeys = append(acceptedKeys, key)
			}
			sort.Strings(acceptedKeys)
			base += "\x00" + workspacepkg.DiskContentHash(strings.Join(acceptedKeys, "\x00"))
		}
		ordinal := ordinals[base]
		ordinals[base] = ordinal + 1
		declarationFingerprint := workspacepkg.DiskContentHash(base + "\x00" + strconv.Itoa(ordinal))
		nameFingerprint := nameFingerprints[strings.ToLower(declaration.Name)]
		queryKey := workspacepkg.DiskContentHash("workspace-reference-query-v1\x00" + origin + "\x00" + declarationFingerprint + "\x00" + nameFingerprint + "\x00" + scopeFingerprint + "\x00" + settingsFingerprint)
		descriptors = append(descriptors, workspaceReferenceQueryDescriptor{
			key: []byte(queryKey), declaration: declaration, declarationFingerprint: declarationFingerprint,
			nameFingerprint: nameFingerprint, scopeFingerprint: scopeFingerprint,
		})
	}
	return descriptors
}

func (s *Server) workspaceReferenceDescriptorFingerprintsContext(ctx context.Context, parsed *core.ParsedDocument, declarations []vbUsageDeclaration, documents []*core.ParsedDocument) (map[string]string, string) {
	if ctx.Err() != nil {
		return nil, ""
	}
	origin := workspacepkg.FileIdentityKeyFromURI(parsed.URI)
	requested := make([]string, 0, len(declarations))
	seen := make(map[string]struct{}, len(declarations))
	for _, declaration := range declarations {
		name := strings.ToLower(declaration.Name)
		if _, duplicate := seen[name]; duplicate {
			continue
		}
		seen[name] = struct{}{}
		requested = append(requested, name)
	}

	s.mu.Lock()
	generation := s.referenceGeneration
	cacheKey := origin + "#" + strconv.FormatUint(generation, 10)
	cached := s.referenceDescriptorFingerprints[cacheKey]
	if cached == nil {
		cached = &workspaceReferenceDescriptorFingerprintCache{names: map[string]workspaceReferenceCachedNameFingerprint{}}
		s.referenceDescriptorFingerprints[cacheKey] = cached
	}
	nameFingerprints := make(map[string]string, len(requested))
	missing := make([]string, 0, len(requested))
	revisions := make(map[string]uint64, len(requested))
	for _, name := range requested {
		revision := s.referenceNameRevisions[name]
		revisions[name] = revision
		if entry, ok := cached.names[name]; ok && entry.revision == revision {
			nameFingerprints[name] = entry.fingerprint
		} else {
			missing = append(missing, name)
		}
	}
	scopeFingerprint := cached.scope
	s.mu.Unlock()

	if len(missing) > 0 {
		s.workspaceIndexStateMu.RLock()
		computed := s.referenceWorkspaceIndex.semanticFingerprintsForNamesContext(ctx, missing, documents)
		s.workspaceIndexStateMu.RUnlock()
		if ctx.Err() != nil {
			return nil, ""
		}
		for name, fingerprint := range computed {
			nameFingerprints[name] = fingerprint
		}
	}
	if scopeFingerprint == "" {
		scopeParts := make([]string, 0, len(documents))
		for _, document := range documents {
			if document == nil {
				continue
			}
			scopeParts = append(scopeParts, workspacepkg.FileIdentityKeyFromURI(document.URI)+"\x00"+workspaceReferenceIncludeFingerprint(document))
		}
		sort.Strings(scopeParts)
		scopeFingerprint = workspacepkg.DiskContentHash(strings.Join(scopeParts, "\x00"))
	}

	s.mu.Lock()
	if ctx.Err() == nil && s.referenceGeneration == generation {
		cached = s.referenceDescriptorFingerprints[cacheKey]
		if cached == nil {
			cached = &workspaceReferenceDescriptorFingerprintCache{names: map[string]workspaceReferenceCachedNameFingerprint{}}
			s.referenceDescriptorFingerprints[cacheKey] = cached
		}
		if cached.scope == "" {
			cached.scope = scopeFingerprint
		}
		for _, name := range missing {
			if s.referenceNameRevisions[name] == revisions[name] {
				cached.names[name] = workspaceReferenceCachedNameFingerprint{revision: revisions[name], fingerprint: nameFingerprints[name]}
			}
		}
	}
	s.mu.Unlock()
	return nameFingerprints, scopeFingerprint
}

func referenceBatchCacheKey(uri string, _ int, generation uint64) workspaceReferenceBatchKey {
	return workspaceReferenceBatchKey{DocumentKey: workspacepkg.FileIdentityKeyFromURI(uri), Generation: generation}
}

func vbReferencesBatchDelay() time.Duration {
	raw := strings.TrimSpace(os.Getenv("ASP_LSP_TEST_VB_REFERENCES_WORKER_DELAY_MS"))
	if raw == "" {
		return 0
	}
	milliseconds, err := strconv.Atoi(raw)
	if err != nil || milliseconds <= 0 {
		return 0
	}
	return time.Duration(milliseconds) * time.Millisecond
}

func vbCodeLensCurrentDeclarationPosition(parsed *core.ParsedDocument, name string, symbolKind string, fallback lsp.Position) lsp.Position {
	var matches []vbUsageDeclaration
	classLines := vbClassLineSet(parsed)
	for _, declaration := range collectVBNamingDeclarations(parsed) {
		declaration.Kind = vbCodeLensSymbolKind(declaration, classLines)
		if strings.EqualFold(declaration.Name, name) && declaration.Kind == symbolKind {
			matches = append(matches, declaration)
		}
	}
	if len(matches) == 1 {
		return matches[0].Range.Start
	}
	for _, declaration := range matches {
		if declaration.Range.Start == fallback {
			return fallback
		}
	}
	return fallback
}
