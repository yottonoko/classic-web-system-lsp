package lspserver

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

type workspaceReferenceBatchTarget struct {
	declaration vbUsageDeclaration
	name        string
	callOnly    bool
	unqualified bool
	implicit    map[string]struct{}
}

type workspaceReferenceBatchResult struct {
	count int
	stale bool
}

type workspaceReferenceCountDelta struct {
	TargetIndex int
	Count       int
}

type workspaceReferenceBatchProgress struct {
	Segments  bool
	URI       string
	Deltas    []workspaceReferenceCountDelta
	Completed int
	Total     int
}

type workspaceReferenceBatchWork struct {
	name        string
	segments    []*workspaceReferenceDocumentSegment
	planIndexes []int
}

type workspaceReferenceBatchPlan struct {
	target                  workspaceReferenceBatchTarget
	targetIndex             int
	additionalTargetIndexes []int
}

type workspaceReferenceAggregateProgress struct {
	uri       string
	counts    []int
	completed int
}

type workspaceReferenceAggregateResult struct {
	counts    []int
	shadowed  []int
	documents int
	progress  []workspaceReferenceAggregateProgress
	cancelled bool
}

type workspaceReferenceBatchPlanKey struct {
	name                string
	callOnly            bool
	unqualified         bool
	implicit            bool
	implicitIdentity    workspaceReferencePreviousKey
	rangeStartLine      int
	rangeStartCharacter int
	rangeEndLine        int
	rangeEndCharacter   int
}

const (
	workspaceReferenceParallelWorkThreshold = 4_096
	workspaceReferenceProgressSegmentBatch  = 32
)

type workspaceReferenceProgressCoalescer struct {
	mu        sync.Mutex
	callback  func(workspaceReferenceBatchProgress)
	total     int
	completed int
	pending   int
	uri       string
	deltas    map[int]int
}

func newWorkspaceReferenceProgressCoalescer(total int, callback func(workspaceReferenceBatchProgress)) *workspaceReferenceProgressCoalescer {
	if callback == nil {
		return nil
	}
	return &workspaceReferenceProgressCoalescer{callback: callback, total: total, deltas: map[int]int{}}
}

func (c *workspaceReferenceProgressCoalescer) addPlans(uri string, completed int, planIndexes, counts []int, plans []workspaceReferenceBatchPlan) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.completed += completed
	c.pending += completed
	c.uri = uri
	for localIndex, planIndex := range planIndexes {
		count := counts[localIndex]
		if count == 0 {
			continue
		}
		plan := plans[planIndex]
		c.deltas[plan.targetIndex] += count
		for _, targetIndex := range plan.additionalTargetIndexes {
			c.deltas[targetIndex] += count
		}
	}
	if c.pending >= workspaceReferenceProgressSegmentBatch || c.completed == c.total {
		c.publishLocked()
	}
}

func (c *workspaceReferenceProgressCoalescer) flush() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.publishLocked()
}

func (c *workspaceReferenceProgressCoalescer) publishLocked() {
	if c.pending == 0 {
		return
	}
	targetIndexes := make([]int, 0, len(c.deltas))
	for targetIndex := range c.deltas {
		targetIndexes = append(targetIndexes, targetIndex)
	}
	sort.Ints(targetIndexes)
	deltas := make([]workspaceReferenceCountDelta, 0, len(targetIndexes))
	for _, targetIndex := range targetIndexes {
		if count := c.deltas[targetIndex]; count != 0 {
			deltas = append(deltas, workspaceReferenceCountDelta{TargetIndex: targetIndex, Count: count})
		}
	}
	c.callback(workspaceReferenceBatchProgress{Segments: true, URI: c.uri, Deltas: deltas, Completed: c.completed, Total: c.total})
	clear(c.deltas)
	c.pending = 0
}

// workspaceVBScriptReferenceBatch counts declarations from matching inverted
// segments. Hot names are divided into flat worker chunks without nested work.
func (s *Server) workspaceVBScriptReferenceBatch(ctx context.Context, parsed *core.ParsedDocument, declarations []vbUsageDeclaration, documents []*core.ParsedDocument, generation uint64, progress func(workspaceReferenceBatchProgress)) []workspaceReferenceBatchResult {
	return s.workspaceVBScriptReferenceBatchMode(ctx, parsed, declarations, documents, generation, progress, true)
}

func (s *Server) workspaceVBScriptReferenceBatchMode(ctx context.Context, parsed *core.ParsedDocument, declarations []vbUsageDeclaration, documents []*core.ParsedDocument, generation uint64, progress func(workspaceReferenceBatchProgress), aggregateNames bool) []workspaceReferenceBatchResult {
	results := make([]workspaceReferenceBatchResult, len(declarations))
	if parsed == nil || len(declarations) == 0 || len(documents) == 0 {
		return results
	}
	plansByName := make(map[string][]int, len(declarations))
	plans := make([]workspaceReferenceBatchPlan, 0, len(declarations))
	planIndexesByKey := make(map[workspaceReferenceBatchPlanKey]int, len(declarations))
	implicitByIdentity := map[workspaceReferencePreviousKey]map[string]struct{}{}
	classLines := vbClassLineSet(parsed)
	for index, declaration := range declarations {
		name := strings.ToLower(declaration.Name)
		_, inClass := classLines[declaration.Line]
		target := workspaceReferenceBatchTarget{
			declaration: declaration,
			name:        name,
			callOnly:    vbReferenceUsesCallRanges(declaration.Kind),
			unqualified: declaration.MemberOf == "" && !inClass,
		}
		key := workspaceReferenceBatchPlanKey{name: name, callOnly: target.callOnly, unqualified: target.unqualified}
		if declaration.Implicit {
			identity := workspaceReferencePreviousCountKey(parsed, declaration)
			target.implicit = implicitByIdentity[identity]
			if target.implicit == nil {
				target.implicit = s.implicitGlobalReferenceDocumentKeys(parsed, declaration)
				implicitByIdentity[identity] = target.implicit
			}
			key.implicit = true
			key.implicitIdentity = identity
			key.rangeStartLine = declaration.Range.Start.Line
			key.rangeStartCharacter = declaration.Range.Start.Character
			key.rangeEndLine = declaration.Range.End.Line
			key.rangeEndCharacter = declaration.Range.End.Character
		}
		if planIndex, ok := planIndexesByKey[key]; ok {
			plans[planIndex].additionalTargetIndexes = append(plans[planIndex].additionalTargetIndexes, index)
			continue
		}
		planIndex := len(plans)
		planIndexesByKey[key] = planIndex
		plans = append(plans, workspaceReferenceBatchPlan{target: target, targetIndex: index})
		plansByName[name] = append(plansByName[name], planIndex)
	}

	names := make([]string, 0, len(plansByName))
	for name := range plansByName {
		names = append(names, name)
	}
	sort.Strings(names)
	useAggregate := aggregateNames && len(plans) > 1
	for _, plan := range plans {
		if plan.target.implicit != nil {
			useAggregate = false
			break
		}
	}
	if useAggregate {
		aggregate := s.aggregateWorkspaceReferenceBatch(ctx, parsed, documents, plans, progress != nil)
		if aggregate.cancelled || ctx.Err() != nil {
			for index := range results {
				results[index].stale = true
			}
			return results
		}
		if progress != nil {
			for _, update := range aggregate.progress {
				deltas := make([]workspaceReferenceCountDelta, 0, len(plans))
				for planIndex, count := range update.counts {
					if count == 0 {
						continue
					}
					plan := plans[planIndex]
					deltas = append(deltas, workspaceReferenceCountDelta{TargetIndex: plan.targetIndex, Count: count})
					for _, targetIndex := range plan.additionalTargetIndexes {
						deltas = append(deltas, workspaceReferenceCountDelta{TargetIndex: targetIndex, Count: count})
					}
				}
				sort.Slice(deltas, func(i, j int) bool { return deltas[i].TargetIndex < deltas[j].TargetIndex })
				progress(workspaceReferenceBatchProgress{URI: update.uri, Deltas: deltas, Completed: update.completed, Total: aggregate.documents})
				if ctx.Err() != nil {
					for index := range results {
						results[index].stale = true
					}
					return results
				}
			}
		}
		s.mu.Lock()
		current := s.referenceGeneration == generation
		s.mu.Unlock()
		if !current {
			for index := range results {
				results[index].stale = true
			}
			return results
		}
		shadowed := 0
		for planIndex, plan := range plans {
			count := aggregate.counts[planIndex]
			results[plan.targetIndex].count = count
			for _, targetIndex := range plan.additionalTargetIndexes {
				results[targetIndex].count = count
			}
			shadowed += aggregate.shadowed[planIndex] * (1 + len(plan.additionalTargetIndexes))
		}
		if shadowed > 0 {
			s.logDebugSummaryEvent("referenceCache.shadowed", "[asp-lsp] vb.references.batch.shadowed documents="+strconv.Itoa(shadowed), map[string]any{"documents": shadowed})
		}
		return results
	}
	segmentsByName := s.referenceWorkspaceIndex.segmentsForNamesContext(ctx, names, documents)
	workers := workspaceReferenceBatchWorkerCount(s.analysisWorkers.workerCount(), names, segmentsByName, plansByName)
	work := partitionWorkspaceReferenceBatchWork(names, segmentsByName, plansByName, workers)
	totalSegments := 0
	for _, item := range work {
		totalSegments += len(item.segments)
	}
	planCounts := make([]atomic.Int64, len(plans))
	parsedKey := workspacepkg.FileIdentityKeyFromURI(parsed.URI)
	var shadowed atomic.Int64
	progressCoalescer := newWorkspaceReferenceProgressCoalescer(totalSegments, progress)
	s.analysisWorkers.parallelForBulk(ctx, len(work), func(workerCtx context.Context, workIndex int) {
		item := work[workIndex]
		local := make([]int, len(item.planIndexes))
		var progressCounts []int
		if progressCoalescer != nil {
			progressCounts = make([]int, len(item.planIndexes))
		}
		progressCompleted := 0
		progressURI := ""
		flushProgress := func() {
			if progressCompleted == 0 {
				return
			}
			progressCoalescer.addPlans(progressURI, progressCompleted, item.planIndexes, progressCounts, plans)
			clear(progressCounts)
			progressCompleted = 0
		}
		for _, segment := range item.segments {
			if workerCtx.Err() != nil || segment == nil {
				return
			}
			for localIndex, planIndex := range item.planIndexes {
				plan := plans[planIndex]
				target := plan.target
				if target.implicit == nil && segment.documentKey != parsedKey && segment.counts.Declarations > 0 {
					shadowed.Add(int64(1 + len(plan.additionalTargetIndexes)))
					continue
				}
				if target.implicit != nil {
					if _, accepted := target.implicit[segment.documentKey]; !accepted {
						continue
					}
				}
				count := workspaceReferenceSegmentCount(segment, target, parsedKey)
				if count == 0 {
					continue
				}
				local[localIndex] += count
				if progressCounts != nil {
					progressCounts[localIndex] += count
				}
			}
			if progressCounts != nil {
				progressCompleted++
				progressURI = segment.parsed.URI
				if progressCompleted == workspaceReferenceProgressSegmentBatch {
					flushProgress()
				}
			}
		}
		flushProgress()
		for localIndex, planIndex := range item.planIndexes {
			planCounts[planIndex].Add(int64(local[localIndex]))
		}
	})
	progressCoalescer.flush()

	if ctx.Err() != nil {
		for index := range results {
			results[index].stale = true
		}
		return results
	}
	s.mu.Lock()
	current := s.referenceGeneration == generation
	s.mu.Unlock()
	if !current {
		for index := range results {
			results[index].stale = true
		}
		return results
	}
	for planIndex, plan := range plans {
		count := int(planCounts[planIndex].Load())
		results[plan.targetIndex].count = count
		for _, targetIndex := range plan.additionalTargetIndexes {
			results[targetIndex].count = count
		}
	}
	if skipped := shadowed.Load(); skipped > 0 {
		s.logDebugSummaryEvent("referenceCache.shadowed", "[asp-lsp] vb.references.batch.shadowed documents="+strconv.FormatInt(skipped, 10), map[string]any{"documents": skipped})
	}
	return results
}

func (s *Server) aggregateWorkspaceReferenceBatch(ctx context.Context, parsed *core.ParsedDocument, documents []*core.ParsedDocument, plans []workspaceReferenceBatchPlan, collectProgress bool) workspaceReferenceAggregateResult {
	result := workspaceReferenceAggregateResult{counts: make([]int, len(plans)), shadowed: make([]int, len(plans))}
	if ctx == nil {
		ctx = context.Background()
	}
	index := s.referenceWorkspaceIndex
	index.updateCountContext(ctx, documents)
	if ctx.Err() != nil {
		result.cancelled = true
		return result
	}

	index.mu.RLock()
	documentIDs := make([]uint64, 0, len(documents))
	var documentURIs []string
	if collectProgress {
		documentURIs = make([]string, 0, len(documents))
	}
	useSeenBits := index.nextDocumentID <= uint64(max(64, len(documents)*8))
	var seenBits []uint64
	var seenMap map[uint64]struct{}
	if useSeenBits {
		seenBits = make([]uint64, (index.nextDocumentID+64)/64)
	} else {
		seenMap = make(map[uint64]struct{}, len(documents))
	}
	for _, document := range documents {
		if document == nil {
			continue
		}
		key := index.parsedDocuments[document]
		if key == "" {
			key = workspacepkg.FileIdentityKeyFromURI(document.URI)
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
		if documentURIs != nil {
			documentURIs = append(documentURIs, document.URI)
		}
	}
	result.documents = len(documentIDs)
	if len(documentIDs) == 0 {
		index.mu.RUnlock()
		return result
	}
	revision := index.countRevision.Load()
	cacheKey := workspaceReferenceAggregateKey(revision, workspacepkg.FileIdentityKeyFromURI(parsed.URI), documentIDs, documentURIs, plans, collectProgress)
	if cached, ok := index.aggregateCache.get(cacheKey); ok {
		index.mu.RUnlock()
		return cached
	}
	type aggregateGroup struct {
		matching    map[uint64]*workspaceReferenceDocumentSegment
		planIndexes []int
	}
	groups := make([]aggregateGroup, 0, len(plans))
	groupIndexes := make(map[string]int, len(plans))
	for planIndex, plan := range plans {
		groupIndex, found := groupIndexes[plan.target.name]
		if !found {
			groupIndex = len(groups)
			groupIndexes[plan.target.name] = groupIndex
			groups = append(groups, aggregateGroup{matching: index.names[plan.target.name]})
		}
		groups[groupIndex].planIndexes = append(groups[groupIndex].planIndexes, planIndex)
	}
	parsedKey := workspacepkg.FileIdentityKeyFromURI(parsed.URI)
	progressChunks := (len(documentIDs) + workspaceReferenceProgressSegmentBatch - 1) / workspaceReferenceProgressSegmentBatch
	var progressCounts []int
	if collectProgress {
		progressCounts = make([]int, progressChunks*len(plans))
	}
	s.analysisWorkers.parallelForBulk(ctx, len(groups), func(workerCtx context.Context, groupIndex int) {
		group := groups[groupIndex]
		for documentIndex, documentID := range documentIDs {
			if documentIndex%workspaceReferenceProgressSegmentBatch == 0 && workerCtx.Err() != nil {
				return
			}
			segment := group.matching[documentID]
			if segment == nil {
				continue
			}
			for _, planIndex := range group.planIndexes {
				plan := plans[planIndex]
				if segment.documentKey != parsedKey && segment.counts.Declarations > 0 {
					result.shadowed[planIndex]++
					continue
				}
				count := workspaceReferenceSegmentCount(segment, plan.target, parsedKey)
				result.counts[planIndex] += count
				if progressCounts != nil {
					progressCounts[(documentIndex/workspaceReferenceProgressSegmentBatch)*len(plans)+planIndex] += count
				}
			}
		}
	})
	index.mu.RUnlock()
	if ctx.Err() != nil {
		result.cancelled = true
		return result
	}
	if collectProgress && result.documents > 0 {
		result.progress = make([]workspaceReferenceAggregateProgress, progressChunks)
		for chunkIndex := 0; chunkIndex < progressChunks; chunkIndex++ {
			completed := min((chunkIndex+1)*workspaceReferenceProgressSegmentBatch, result.documents)
			counts := progressCounts[chunkIndex*len(plans) : (chunkIndex+1)*len(plans)]
			result.progress[chunkIndex] = workspaceReferenceAggregateProgress{
				uri: documentURIs[completed-1], counts: counts, completed: completed,
			}
		}
	}
	if ctx.Err() == nil && index.countRevision.Load() == revision {
		index.aggregateCache.store(cacheKey, result)
	}
	return result
}

func workspaceReferenceSegmentCount(segment *workspaceReferenceDocumentSegment, target workspaceReferenceBatchTarget, parsedKey string) int {
	count := segment.counts.CodeLensReferences
	if target.callOnly {
		count = segment.counts.CodeLensCalls
	}
	if target.unqualified {
		count = segment.counts.UnqualifiedCodeLensReferences
		if target.callOnly {
			count = segment.counts.UnqualifiedCodeLensCalls
		}
	}
	if target.implicit != nil && segment.documentKey == parsedKey {
		adjustment, knownAdjustment := segment.implicitAdjustments[target.declaration.Range]
		if knownAdjustment {
			if target.unqualified && target.callOnly {
				count -= adjustment.UnqualifiedCodeLensCalls
			} else if target.unqualified {
				count -= adjustment.UnqualifiedCodeLensReferences
			} else if target.callOnly {
				count -= adjustment.CodeLensCalls
			} else {
				count -= adjustment.CodeLensReferences
			}
		} else if segment.postingsLoaded {
			for _, posting := range segment.postings {
				_, alreadyExcluded := segment.declarationRanges[posting.Range]
				if posting.Range == target.declaration.Range && !alreadyExcluded && !posting.HasRole(vbscript.ReferenceRoleDeclaration) && !posting.HasRole(vbscript.ReferenceRoleCref) &&
					!posting.HasRole(vbscript.ReferenceRoleObjectInitialization) && (!target.callOnly || posting.HasRole(vbscript.ReferenceRoleCall)) {
					count--
					break
				}
			}
		}
	}
	return count
}

func workspaceReferenceBatchWorkerCount(maxWorkers int, names []string, segmentsByName map[string][]*workspaceReferenceDocumentSegment, plansByName map[string][]int) int {
	if maxWorkers < 1 {
		return 1
	}
	estimatedWork := 0
	for _, name := range names {
		estimatedWork += len(segmentsByName[name]) * len(plansByName[name])
		if estimatedWork > workspaceReferenceParallelWorkThreshold {
			return maxWorkers
		}
	}
	return 1
}

func partitionWorkspaceReferenceBatchWork(names []string, segmentsByName map[string][]*workspaceReferenceDocumentSegment, plansByName map[string][]int, workers int) []workspaceReferenceBatchWork {
	activeNames := 0
	for _, name := range names {
		if len(segmentsByName[name]) > 0 {
			activeNames++
		}
	}
	work := make([]workspaceReferenceBatchWork, 0, max(len(names), workers))
	for _, name := range names {
		segments := segmentsByName[name]
		if len(segments) == 0 {
			continue
		}
		parts := 1
		if activeNames < workers && len(segments) > 1 {
			parts = min(len(segments), max(1, workers/activeNames))
		}
		chunkSize := max(1, (len(segments)+parts-1)/parts)
		for start := 0; start < len(segments); start += chunkSize {
			end := min(start+chunkSize, len(segments))
			work = append(work, workspaceReferenceBatchWork{name: name, segments: segments[start:end], planIndexes: plansByName[name]})
		}
	}
	return work
}
