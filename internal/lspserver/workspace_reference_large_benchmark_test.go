package lspserver

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

const (
	largeReferenceWorkspaceDocumentCount = 2_000
	largeReferenceWorkspaceUsesPerFile   = 6
	largeReferenceWorkspaceExpectedCount = largeReferenceWorkspaceDocumentCount * largeReferenceWorkspaceUsesPerFile
	referenceCountScaleDocumentCount     = 2_000
	referenceCountScaleVariableCount     = 11
	referenceCountScaleReferencesPerFile = 20
	referenceCountScaleExpectedCount     = referenceCountScaleDocumentCount * referenceCountScaleReferencesPerFile
	referenceCountScaleExpectedTotal     = referenceCountScaleVariableCount * referenceCountScaleExpectedCount
)

func TestReferenceCodeLensCalculatingTitleShowsLatestPartialCount(t *testing.T) {
	counts := []int{0, 1, 1_024, largeReferenceWorkspaceExpectedCount}
	for _, count := range counts {
		count := count
		previous := count + 100
		wantCount := strconv.Itoa(count) + "+"
		english := referenceCodeLensCalculatingTitle(&count, &previous, false)
		japanese := referenceCodeLensCalculatingTitle(&count, &previous, true)
		if !strings.HasPrefix(english, wantCount) || !strings.Contains(english, "previous "+strconv.Itoa(previous)) {
			t.Fatalf("English partial title %q does not include partial %s and previous %d", english, wantCount, previous)
		}
		if !strings.HasPrefix(japanese, wantCount) || !strings.Contains(japanese, "前回 "+strconv.Itoa(previous)) {
			t.Fatalf("Japanese partial title %q does not include partial %s and previous %d", japanese, wantCount, previous)
		}
	}
}

func TestWorkspaceReferenceBatchPartialCountsAreMonotonic(t *testing.T) {
	const documentCount = 128
	parsed, declaration, documents := referenceWorkspaceFixture(t, documentCount)
	server := New(nil, io.Discard, nil)
	configureReferenceBenchmarkWorkers(server, 4)
	partials := make([]int, 0, documentCount)
	completed := make([]int, 0, documentCount/workspaceReferenceProgressSegmentBatch)
	total := 0
	var mu sync.Mutex
	results := server.workspaceVBScriptReferenceBatch(
		context.Background(),
		parsed,
		[]vbUsageDeclaration{declaration},
		documents,
		server.referenceGeneration,
		func(progress workspaceReferenceBatchProgress) {
			mu.Lock()
			for _, delta := range progress.Deltas {
				if delta.TargetIndex == 0 {
					total += delta.Count
				}
			}
			partials = append(partials, total)
			completed = append(completed, progress.Completed)
			mu.Unlock()
		},
	)
	want := documentCount * largeReferenceWorkspaceUsesPerFile
	if len(results) != 1 || results[0].stale || results[0].count != want {
		t.Fatalf("reference batch = %#v, want count %d", results, want)
	}
	wantUpdates := (documentCount + workspaceReferenceProgressSegmentBatch - 1) / workspaceReferenceProgressSegmentBatch
	if len(partials) != wantUpdates {
		t.Fatalf("partial updates = %d, want %d", len(partials), wantUpdates)
	}
	previous := -1
	for index, partial := range partials {
		if partial < previous {
			t.Fatalf("partial update %d decreased from %d to %d", index, previous, partial)
		}
		previous = partial
		if completed[index] <= 0 || completed[index] > documentCount || index > 0 && completed[index] <= completed[index-1] {
			t.Fatalf("completed segments = %#v, want strictly increasing values", completed)
		}
	}
	if previous != want {
		t.Fatalf("last partial count = %d, want %d", previous, want)
	}
}

func TestReferenceCountScaleFixtureHasElevenVariablesFortyThousandReferencesEach(t *testing.T) {
	parsed, declarations, documents := referenceCountScaleFixture(t)
	if len(documents) != referenceCountScaleDocumentCount {
		t.Fatalf("documents = %d, want %d", len(documents), referenceCountScaleDocumentCount)
	}
	if len(declarations) != referenceCountScaleVariableCount {
		t.Fatalf("declarations = %d, want %d", len(declarations), referenceCountScaleVariableCount)
	}
	server := New(nil, io.Discard, nil)
	configureReferenceBenchmarkWorkers(server, 16)
	results := server.workspaceVBScriptReferenceBatch(
		context.Background(),
		parsed,
		declarations,
		documents,
		server.referenceGeneration,
		nil,
	)
	assertReferenceCountScaleResults(t, results)
}

func TestWorkspaceReferenceAggregateBatchPreservesScopeDedupShadowAndProgress(t *testing.T) {
	settings := core.Settings{DefaultLanguage: "VBScript"}
	origin := core.ParseDocument("file:///aggregate-origin.asp", "<% Dim FirstValue, SecondValue : Response.Write FirstValue : Response.Write SecondValue %>", settings)
	other := core.ParseDocument("file:///aggregate-other.asp", "<% Response.Write FirstValue : Response.Write SecondValue %>", settings)
	shadow := core.ParseDocument("file:///aggregate-shadow.asp", "<% Dim FirstValue : Response.Write FirstValue : Response.Write SecondValue %>", settings)
	declarations := New(nil, io.Discard, io.Discard).workspaceReferenceCodeLensPlan(origin).declarations
	if len(declarations) != 2 {
		t.Fatalf("aggregate declarations = %#v, want two", declarations)
	}
	server := New(nil, io.Discard, io.Discard)
	configureReferenceBenchmarkWorkers(server, 4)
	progressCounts := make([]int, len(declarations))
	updates := 0
	results := server.workspaceVBScriptReferenceBatch(
		context.Background(), origin, declarations,
		[]*core.ParsedDocument{origin, other, other, shadow}, server.referenceGeneration,
		func(progress workspaceReferenceBatchProgress) {
			updates++
			if progress.Completed != 3 || progress.Total != 3 {
				t.Fatalf("aggregate progress = %#v, want three unique documents complete", progress)
			}
			for _, delta := range progress.Deltas {
				progressCounts[delta.TargetIndex] += delta.Count
			}
		},
	)
	if updates != 1 {
		t.Fatalf("aggregate progress updates = %d, want one completed update", updates)
	}
	for index, declaration := range declarations {
		want := 3
		if strings.EqualFold(declaration.Name, "FirstValue") {
			want = 2
		}
		if results[index].stale || results[index].count != want || progressCounts[index] != want {
			t.Fatalf("aggregate %s = result %#v progress %d, want %d", declaration.Name, results[index], progressCounts[index], want)
		}
	}
}

func TestWorkspaceReferenceAggregateBatchProgressPreservesCancellation(t *testing.T) {
	const documentCount = 64
	settings := core.Settings{DefaultLanguage: "VBScript"}
	documents := make([]*core.ParsedDocument, documentCount)
	for index := range documents {
		source := "<% Response.Write FirstValue : Response.Write SecondValue %>"
		if index == 0 {
			source = "<% Dim FirstValue, SecondValue : Response.Write FirstValue : Response.Write SecondValue %>"
		}
		documents[index] = core.ParseDocument(fmt.Sprintf("file:///aggregate-cancel-%d.asp", index), source, settings)
	}
	server := New(nil, io.Discard, io.Discard)
	configureReferenceBenchmarkWorkers(server, 4)
	declarations := server.workspaceReferenceCodeLensPlan(documents[0]).declarations
	ctx, cancel := context.WithCancel(context.Background())
	updates := 0
	results := server.workspaceVBScriptReferenceBatch(ctx, documents[0], declarations, documents, server.referenceGeneration, func(progress workspaceReferenceBatchProgress) {
		updates++
		if progress.Completed != workspaceReferenceProgressSegmentBatch || progress.Total != documentCount {
			t.Fatalf("first aggregate progress = %#v, want 32 of 64 documents", progress)
		}
		cancel()
	})
	if updates != 1 {
		t.Fatalf("aggregate progress updates before cancellation = %d, want one", updates)
	}
	for index, result := range results {
		if !result.stale {
			t.Fatalf("cancelled aggregate result %d = %#v, want stale", index, result)
		}
	}
}

func TestWorkspaceReferenceBatchFansOutSharedPlanCountsAndProgress(t *testing.T) {
	const declarationCount = 4
	parsed, declaration, documents := referenceWorkspaceFixture(t, 64)
	declarations := make([]vbUsageDeclaration, declarationCount)
	for index := range declarations {
		declarations[index] = declaration
	}
	server := New(nil, io.Discard, nil)
	progressCounts := make([]int, declarationCount)
	results := server.workspaceVBScriptReferenceBatch(
		context.Background(),
		parsed,
		declarations,
		documents,
		server.referenceGeneration,
		func(progress workspaceReferenceBatchProgress) {
			for _, delta := range progress.Deltas {
				progressCounts[delta.TargetIndex] += delta.Count
			}
		},
	)
	want := len(documents) * largeReferenceWorkspaceUsesPerFile
	for index, result := range results {
		if result.stale || result.count != want {
			t.Fatalf("reference batch result %d = %#v, want count %d", index, result, want)
		}
		if progressCounts[index] != want {
			t.Fatalf("progress count %d = %d, want %d", index, progressCounts[index], want)
		}
	}
}

func TestWorkspaceReferencePartialPublishingStartsImmediatelyAndFlushesFinalCount(t *testing.T) {
	now := time.Unix(100, 0)
	if !shouldPublishWorkspaceReferencePartial(time.Time{}, now, 32, 128) {
		t.Fatal("first partial reference count was not published")
	}
	last := now
	if shouldPublishWorkspaceReferencePartial(last, now.Add(10*time.Millisecond), 64, 128) {
		t.Fatal("partial reference count ignored the refresh interval")
	}
	if !shouldPublishWorkspaceReferencePartial(last, now.Add(10*time.Millisecond), 128, 128) {
		t.Fatal("final partial reference count was not flushed")
	}
	if !shouldPublishWorkspaceReferencePartial(last, now.Add(workspaceReferencePartialPublishInterval), 96, 128) {
		t.Fatal("timed partial reference count was not published")
	}
}

func TestWorkspaceReferenceBatchUsesOneWorkerForSmallSummaryWork(t *testing.T) {
	segments := make([]*workspaceReferenceDocumentSegment, workspaceReferenceParallelWorkThreshold)
	names := []string{"shared"}
	segmentsByName := map[string][]*workspaceReferenceDocumentSegment{"shared": segments}
	targetsByName := map[string][]int{"shared": {0}}
	if got := workspaceReferenceBatchWorkerCount(16, names, segmentsByName, targetsByName); got != 1 {
		t.Fatalf("workers at threshold = %d, want 1", got)
	}
	segmentsByName["shared"] = append(segments, &workspaceReferenceDocumentSegment{})
	if got := workspaceReferenceBatchWorkerCount(16, names, segmentsByName, targetsByName); got != 16 {
		t.Fatalf("workers above threshold = %d, want 16", got)
	}
}

func TestWorkspaceReferenceBatchCoalescedProgressPreservesCancellation(t *testing.T) {
	parsed, declaration, documents := referenceWorkspaceFixture(t, 128)
	server := New(nil, io.Discard, nil)
	configureReferenceBenchmarkWorkers(server, 16)
	ctx, cancel := context.WithCancel(context.Background())
	updates := 0
	results := server.workspaceVBScriptReferenceBatch(
		ctx,
		parsed,
		[]vbUsageDeclaration{declaration},
		documents,
		server.referenceGeneration,
		func(progress workspaceReferenceBatchProgress) {
			updates++
			if progress.Completed != workspaceReferenceProgressSegmentBatch {
				t.Fatalf("first completed segments = %d, want %d", progress.Completed, workspaceReferenceProgressSegmentBatch)
			}
			cancel()
		},
	)
	if updates != 1 {
		t.Fatalf("progress updates = %d, want 1 before cancellation", updates)
	}
	if len(results) != 1 || !results[0].stale {
		t.Fatalf("cancelled reference batch = %#v, want one stale result", results)
	}
}

func TestWorkspaceReferenceIndexSummarizesHotSymbolWithoutLocations(t *testing.T) {
	const documentCount = 128
	_, _, documents := referenceWorkspaceFixture(t, documentCount)
	segments := newWorkspaceReferenceIndex().segmentsForName("SHAREDVALUE", documents)
	if len(segments) != documentCount {
		t.Fatalf("hot-symbol segments = %d, want %d", len(segments), documentCount)
	}
	count := 0
	for _, segment := range segments {
		count += segment.counts.Total - segment.counts.Declarations - segment.counts.Crefs
	}
	want := documentCount * largeReferenceWorkspaceUsesPerFile
	if count != want {
		t.Fatalf("summarized hot-symbol references = %d, want %d", count, want)
	}
}

func TestWorkspaceReferenceBatchPartitionsHotNameDespiteMissingNames(t *testing.T) {
	const workers = 16
	names := make([]string, 1_000)
	names[0] = "shared"
	segments := make([]*workspaceReferenceDocumentSegment, 64)
	for index := range segments {
		segments[index] = &workspaceReferenceDocumentSegment{}
	}
	segmentsByName := map[string][]*workspaceReferenceDocumentSegment{"shared": segments}
	targetsByName := map[string][]int{"shared": {0}}
	for index := 1; index < len(names); index++ {
		names[index] = fmt.Sprintf("missing%d", index)
		targetsByName[names[index]] = []int{index}
	}
	work := partitionWorkspaceReferenceBatchWork(names, segmentsByName, targetsByName, workers)
	if len(work) != workers {
		t.Fatalf("hot-name work items = %d, want %d despite missing names", len(work), workers)
	}
}

func BenchmarkWorkspaceReferenceBatchLargeWorkspace(b *testing.B) {
	parsed, declaration, documents := largeReferenceWorkspaceFixture(b)
	for _, declarationCount := range []int{1, 100, 1_000} {
		declarations := make([]vbUsageDeclaration, declarationCount)
		declarations[0] = declaration
		for index := 1; index < declarationCount; index++ {
			declarations[index] = vbUsageDeclaration{Name: fmt.Sprintf("Missing%d", index), Kind: "variable"}
		}
		for _, workers := range []int{1, 4, 16} {
			b.Run(fmt.Sprintf("declarations-%d/workers-%d", declarationCount, workers), func(b *testing.B) {
				server := New(nil, io.Discard, nil)
				configureReferenceBenchmarkWorkers(server, workers)
				server.referenceWorkspaceIndex.update(documents)
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					results := server.workspaceVBScriptReferenceBatch(
						context.Background(),
						parsed,
						declarations,
						documents,
						server.referenceGeneration,
						nil,
					)
					if len(results) != len(declarations) || results[0].stale || results[0].count != largeReferenceWorkspaceExpectedCount {
						b.Fatalf("reference batch = %#v, want count %d", results, largeReferenceWorkspaceExpectedCount)
					}
				}
			})
		}
	}
}

func BenchmarkWorkspaceReferenceCountElevenVariablesFortyThousandEach(b *testing.B) {
	parsed, declarations, documents := referenceCountScaleFixture(b)
	run := func(b *testing.B, server *Server, aggregateNames bool) {
		results := server.workspaceVBScriptReferenceBatchMode(
			context.Background(),
			parsed,
			declarations,
			documents,
			server.referenceGeneration,
			nil,
			aggregateNames,
		)
		assertReferenceCountScaleResults(b, results)
	}

	b.Run("cold-index-build", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			server := New(nil, io.Discard, nil)
			configureReferenceBenchmarkWorkers(server, 16)
			run(b, server, true)
		}
	})
	b.Run("warm-segmented-baseline", func(b *testing.B) {
		server := New(nil, io.Discard, nil)
		configureReferenceBenchmarkWorkers(server, 16)
		run(b, server, false)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			run(b, server, false)
		}
	})
	b.Run("warm-index-reuse", func(b *testing.B) {
		server := New(nil, io.Discard, nil)
		configureReferenceBenchmarkWorkers(server, 16)
		run(b, server, true)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			run(b, server, true)
		}
	})
}

func BenchmarkWorkspaceReferenceBatchRepeatedNames(b *testing.B) {
	parsed, declaration, documents := referenceWorkspaceFixture(b, 512)
	declarations := make([]vbUsageDeclaration, 128)
	for index := range declarations {
		declarations[index] = declaration
	}
	benchmarkWorkspaceReferenceBatchPlans(b, parsed, declarations, documents, false)
}

func BenchmarkWorkspaceReferenceBatchPresentMissingMix(b *testing.B) {
	parsed, declaration, documents := referenceWorkspaceFixture(b, 512)
	declarations := make([]vbUsageDeclaration, 128)
	for index := range declarations[:32] {
		declarations[index] = declaration
	}
	for index := 32; index < len(declarations); index++ {
		declarations[index] = vbUsageDeclaration{Name: fmt.Sprintf("Missing%d", index), Kind: "variable"}
	}
	benchmarkWorkspaceReferenceBatchPlans(b, parsed, declarations, documents, false)
}

func BenchmarkWorkspaceReferenceBatchProgressEnabled(b *testing.B) {
	parsed, declaration, documents := referenceWorkspaceFixture(b, 512)
	declarations := make([]vbUsageDeclaration, 128)
	for index := range declarations {
		declarations[index] = declaration
	}
	benchmarkWorkspaceReferenceBatchPlans(b, parsed, declarations, documents, true)
}

func benchmarkWorkspaceReferenceBatchPlans(b *testing.B, parsed *core.ParsedDocument, declarations []vbUsageDeclaration, documents []*core.ParsedDocument, progressEnabled bool) {
	b.Helper()
	server := New(nil, io.Discard, nil)
	configureReferenceBenchmarkWorkers(server, 16)
	server.referenceWorkspaceIndex.update(documents)
	want := len(documents) * largeReferenceWorkspaceUsesPerFile
	var progressTotal atomic.Int64
	var progress func(workspaceReferenceBatchProgress)
	if progressEnabled {
		progress = func(update workspaceReferenceBatchProgress) {
			for _, delta := range update.Deltas {
				progressTotal.Add(int64(delta.Count))
			}
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		results := server.workspaceVBScriptReferenceBatch(
			context.Background(),
			parsed,
			declarations,
			documents,
			server.referenceGeneration,
			progress,
		)
		if len(results) != len(declarations) || len(results) == 0 || results[0].stale || results[0].count != want {
			b.Fatalf("reference batch = %#v, want %d results with count %d", results, len(declarations), want)
		}
	}
	if progressEnabled && progressTotal.Load() == 0 {
		b.Fatal("reference batch did not publish progress deltas")
	}
}

func configureReferenceBenchmarkWorkers(server *Server, workers int) {
	pool := &analysisWorkerPool{}
	pool.setWorkers(workers)
	server.analysisWorkers = pool
	server.referenceWorkspaceIndex.setWorkerPool(pool)
}

func BenchmarkWorkspaceReferenceIndexLargeWorkspace(b *testing.B) {
	_, _, documents := largeReferenceWorkspaceFixture(b)
	b.Run("cold", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			index := newWorkspaceReferenceIndex()
			matching := index.documentsForName("SharedValue", documents)
			if len(matching) != largeReferenceWorkspaceDocumentCount {
				b.Fatalf("matching documents = %d, want %d", len(matching), largeReferenceWorkspaceDocumentCount)
			}
		}
	})
	b.Run("warm", func(b *testing.B) {
		index := newWorkspaceReferenceIndex()
		if matching := index.documentsForName("SharedValue", documents); len(matching) != largeReferenceWorkspaceDocumentCount {
			b.Fatalf("matching documents = %d, want %d", len(matching), largeReferenceWorkspaceDocumentCount)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			matching := index.documentsForName("SharedValue", documents)
			if len(matching) != largeReferenceWorkspaceDocumentCount {
				b.Fatalf("matching documents = %d, want %d", len(matching), largeReferenceWorkspaceDocumentCount)
			}
		}
	})
}

func BenchmarkWorkspaceReferenceIndexPublish(b *testing.B) {
	for _, documentCount := range []int{2_000, 10_000} {
		_, _, documents := referenceWorkspaceFixture(b, documentCount)
		for _, workers := range []int{1, 4, 16} {
			b.Run(fmt.Sprintf("documents-%d/workers-%d", documentCount, workers), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					index := newWorkspaceReferenceIndex()
					pool := &analysisWorkerPool{}
					pool.setWorkers(workers)
					index.setWorkerPool(pool)
					index.update(documents)
				}
			})
		}
	}
}

func BenchmarkWorkspaceReferenceIndexSingleDocumentEdit10K(b *testing.B) {
	const documentCount = 10_000
	_, _, documents := referenceWorkspaceFixture(b, documentCount)
	changed := []*core.ParsedDocument{
		core.ParseDocument(
			documents[documentCount/2].URI,
			"<%\nResponse.Write SharedValue\nResponse.Write SharedValue\n%>",
			core.Settings{DefaultLanguage: "VBScript"},
		),
		documents[documentCount/2],
	}
	for _, document := range changed {
		_ = vbscript.BuildReferenceShard(document)
	}
	for _, workers := range []int{1, 4, 16} {
		b.Run(fmt.Sprintf("workers-%d", workers), func(b *testing.B) {
			index := newWorkspaceReferenceIndex()
			pool := &analysisWorkerPool{}
			pool.setWorkers(workers)
			index.setWorkerPool(pool)
			index.update(documents)
			iteration := 0
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				index.update([]*core.ParsedDocument{changed[iteration%len(changed)]})
				iteration++
			}
		})
	}
}

func BenchmarkWorkspaceReferenceSegmentsLargeWorkspace(b *testing.B) {
	_, _, documents := largeReferenceWorkspaceFixture(b)
	index := newWorkspaceReferenceIndex()
	if segments := index.segmentsForName("SharedValue", documents); len(segments) != largeReferenceWorkspaceDocumentCount {
		b.Fatalf("hot-symbol segments = %d, want %d", len(segments), largeReferenceWorkspaceDocumentCount)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		segments := index.segmentsForName("SharedValue", documents)
		count := 0
		for _, segment := range segments {
			count += segment.counts.Total - segment.counts.Declarations - segment.counts.Crefs
		}
		if count != largeReferenceWorkspaceExpectedCount {
			b.Fatalf("summarized hot-symbol references = %d, want %d", count, largeReferenceWorkspaceExpectedCount)
		}
	}
}

func BenchmarkWorkspaceReferenceSegmentsByDensity(b *testing.B) {
	const documentCount = 2_000
	documents := make([]*core.ParsedDocument, documentCount)
	for position := range documents {
		var source strings.Builder
		source.WriteString("<% always = 1")
		if position%1_000 == 0 {
			source.WriteString(" : sparsepointone = 1")
		}
		if position%100 == 0 {
			source.WriteString(" : sparseone = 1")
		}
		if position%10 == 0 {
			source.WriteString(" : sparseten = 1")
		}
		source.WriteString(" %>")
		documents[position] = core.ParseDocument(
			"file:///bench/density-"+strconv.Itoa(position)+".asp",
			source.String(),
			core.Settings{DefaultLanguage: "VBScript"},
		)
	}
	index := newWorkspaceReferenceIndex()
	index.updateCountContext(context.Background(), documents)
	benchmarks := []struct {
		name string
		want int
	}{
		{name: "missing", want: 0},
		{name: "sparsepointone", want: 2},
		{name: "sparseone", want: 20},
		{name: "sparseten", want: 200},
		{name: "always", want: 2_000},
	}
	for _, benchmark := range benchmarks {
		b.Run(benchmark.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				segments := index.segmentsForNamesContext(context.Background(), []string{benchmark.name}, documents)[benchmark.name]
				if len(segments) != benchmark.want {
					b.Fatalf("segments = %d, want %d", len(segments), benchmark.want)
				}
			}
		})
	}
}

func BenchmarkWorkspaceReferenceSegmentsAfterDocumentIDChurn(b *testing.B) {
	_, _, documents := referenceWorkspaceFixture(b, 2_000)
	index := newWorkspaceReferenceIndex()
	index.updateCountContext(context.Background(), documents)
	index.mu.Lock()
	index.nextDocumentID = 1 << 40
	index.mu.Unlock()

	b.ReportAllocs()
	for b.Loop() {
		segments := index.segmentsForNamesContext(context.Background(), []string{"sharedvalue"}, documents)["sharedvalue"]
		if len(segments) != len(documents) {
			b.Fatalf("segments = %d, want %d", len(segments), len(documents))
		}
	}
}

func BenchmarkWorkspaceReferenceCodeLensCountCacheHit100Symbols(b *testing.B) {
	const declarationCount = 100
	parsed := core.ParseDocument("file:///bench/cache-hit.asp", "<% Dim SharedValue %>", core.Settings{DefaultLanguage: "VBScript"})
	declarations := make([]vbUsageDeclaration, declarationCount)
	server := New(nil, io.Discard, nil)
	for index := range declarations {
		declaration := vbUsageDeclaration{
			Name:  fmt.Sprintf("SharedValue%d", index),
			Kind:  "variable",
			Range: lsp.Range{Start: lsp.Position{Line: index, Character: 4}},
		}
		declarations[index] = declaration
		key := workspaceReferenceRequestKey(parsed.URI, declaration.Range.Start, false, declaration.Kind, server.referenceGeneration, declaration.Name)
		server.referenceCounts[key] = largeReferenceWorkspaceExpectedCount
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		states := server.snapshotWorkspaceReferenceCodeLensCounts(parsed, declarations)
		for _, state := range states {
			if !state.final || state.count != largeReferenceWorkspaceExpectedCount {
				b.Fatalf("cached count = %d, %v; want %d, true", state.count, state.final, largeReferenceWorkspaceExpectedCount)
			}
		}
	}
}

func BenchmarkWorkspaceReferenceCodeLensCountBatchVector100Symbols(b *testing.B) {
	const declarationCount = 100
	parsed := core.ParseDocument("file:///bench/batch-vector.asp", "<% Dim SharedValue %>", core.Settings{DefaultLanguage: "VBScript"})
	declarations := make([]vbUsageDeclaration, declarationCount)
	counts := make([]int, declarationCount)
	server := New(nil, io.Discard, nil)
	for index := range declarations {
		declarations[index] = vbUsageDeclaration{
			Name:  fmt.Sprintf("SharedValue%d", index),
			Kind:  "variable",
			Range: lsp.Range{Start: lsp.Position{Line: index, Character: 4}},
		}
		counts[index] = largeReferenceWorkspaceExpectedCount
	}
	server.referenceBatch[referenceBatchCacheKey(parsed.URI, 0, server.referenceGeneration)] = &workspaceReferenceBatchState{
		generation:   server.referenceGeneration,
		complete:     true,
		declarations: declarations,
		finalCounts:  counts,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		states := server.snapshotWorkspaceReferenceCodeLensCounts(parsed, declarations)
		for _, state := range states {
			if !state.final || state.count != largeReferenceWorkspaceExpectedCount {
				b.Fatalf("cached count = %d, %v; want %d, true", state.count, state.final, largeReferenceWorkspaceExpectedCount)
			}
		}
	}
}

func BenchmarkWorkspaceReferenceLocationsLargeWorkspace(b *testing.B) {
	parsed, declaration, documents := largeReferenceWorkspaceFixture(b)
	server := New(nil, io.Discard, nil)
	server.referenceWorkspaceIndex.update(documents)
	run := func() []lsp.Location {
		locations, stale := server.workspaceVBScriptReferencesOnce(
			context.Background(), parsed, declaration.Range.Start, false, "reference:variable", false, documents, true,
		)
		if stale || len(locations) != largeReferenceWorkspaceExpectedCount {
			b.Fatalf("locations = %d, stale=%t; want %d, false", len(locations), stale, largeReferenceWorkspaceExpectedCount)
		}
		return locations
	}
	b.Run("cold-materialization", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			server.mu.Lock()
			server.referenceGeneration++
			server.referenceCounts = map[workspaceReferenceTargetKey]int{}
			server.referenceResults = map[workspaceReferenceTargetKey][]lsp.Location{}
			server.referenceInflight = map[workspaceReferenceTargetKey]*workspaceReferenceInflight{}
			server.mu.Unlock()
			_ = run()
		}
	})
	b.Run("warm-cache", func(b *testing.B) {
		_ = run()
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_ = run()
		}
	})
}

func largeReferenceWorkspaceFixture(tb testing.TB) (*core.ParsedDocument, vbUsageDeclaration, []*core.ParsedDocument) {
	return referenceWorkspaceFixture(tb, largeReferenceWorkspaceDocumentCount)
}

func referenceWorkspaceFixture(tb testing.TB, documentCount int) (*core.ParsedDocument, vbUsageDeclaration, []*core.ParsedDocument) {
	tb.Helper()
	documents := make([]*core.ParsedDocument, documentCount)
	for index := range documents {
		var source string
		if index == 0 {
			source = "<%\nDim SharedValue\n"
		} else {
			source = "<%\n"
		}
		for occurrence := 0; occurrence < largeReferenceWorkspaceUsesPerFile; occurrence++ {
			source += "Response.Write SharedValue\n"
		}
		source += "%>"
		documents[index] = core.ParseDocument(
			fmt.Sprintf("file:///bench/reference-workspace/%04d.asp", index),
			source,
			core.Settings{DefaultLanguage: "VBScript"},
		)
		_ = vbscript.BuildReferenceShard(documents[index])
	}
	declarations := collectVBUsageDeclarations(documents[0]).Declarations
	for _, declaration := range declarations {
		if strings.EqualFold(declaration.Name, "SharedValue") {
			return documents[0], declaration, documents
		}
	}
	tb.Fatal("SharedValue declaration was not found in benchmark fixture")
	return nil, vbUsageDeclaration{}, nil
}

func referenceCountScaleFixture(tb testing.TB) (*core.ParsedDocument, []vbUsageDeclaration, []*core.ParsedDocument) {
	tb.Helper()
	names := make([]string, referenceCountScaleVariableCount)
	names[0] = "SharedValue"
	for index := 1; index < len(names); index++ {
		names[index] = fmt.Sprintf("SharedValue%d", index)
	}
	documents := make([]*core.ParsedDocument, referenceCountScaleDocumentCount)
	for index := range documents {
		var source strings.Builder
		source.WriteString("<%\n")
		if index == 0 {
			for _, name := range names {
				source.WriteString("Dim ")
				source.WriteString(name)
				source.WriteByte('\n')
			}
		}
		for _, name := range names {
			for range referenceCountScaleReferencesPerFile {
				source.WriteString("Response.Write ")
				source.WriteString(name)
				source.WriteByte('\n')
			}
		}
		source.WriteString("%>")
		documents[index] = core.ParseDocument(
			fmt.Sprintf("file:///bench/reference-count-scale/%04d.asp", index),
			source.String(),
			core.Settings{DefaultLanguage: "VBScript"},
		)
		_ = vbscript.BuildReferenceShard(documents[index])
	}
	declarationsByName := make(map[string]vbUsageDeclaration, referenceCountScaleVariableCount)
	for _, declaration := range collectVBUsageDeclarations(documents[0]).Declarations {
		declarationsByName[strings.ToLower(declaration.Name)] = declaration
	}
	declarations := make([]vbUsageDeclaration, len(names))
	for index, name := range names {
		declaration, ok := declarationsByName[strings.ToLower(name)]
		if !ok {
			tb.Fatalf("%s declaration was not found in reference count scale fixture", name)
		}
		declarations[index] = declaration
	}
	return documents[0], declarations, documents
}

func assertReferenceCountScaleResults(tb testing.TB, results []workspaceReferenceBatchResult) {
	tb.Helper()
	if len(results) != referenceCountScaleVariableCount {
		tb.Fatalf("reference batch results = %d, want %d", len(results), referenceCountScaleVariableCount)
	}
	total := 0
	for _, result := range results {
		if result.stale || result.count != referenceCountScaleExpectedCount {
			tb.Fatalf("reference batch result = %#v, want count %d and not stale", result, referenceCountScaleExpectedCount)
		}
		total += result.count
	}
	if total != referenceCountScaleExpectedTotal {
		tb.Fatalf("total references = %d, want %d", total, referenceCountScaleExpectedTotal)
	}
}
