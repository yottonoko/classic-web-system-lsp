package lspserver

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestInteractiveAnalysisBypassesSaturatedBulkWorkers(t *testing.T) {
	pool := &analysisWorkerPool{}
	pool.setWorkers(2)
	bulkStarted := make(chan struct{})
	releaseBulk := make(chan struct{})
	bulkDone := make(chan struct{})
	var startedOnce sync.Once
	go func() {
		defer close(bulkDone)
		pool.parallelForBulk(context.Background(), 4, func(context.Context, int) {
			startedOnce.Do(func() { close(bulkStarted) })
			<-releaseBulk
		})
	}()
	select {
	case <-bulkStarted:
	case <-time.After(time.Second):
		t.Fatal("bulk analysis did not start")
	}

	interactiveDone := make(chan struct{})
	go pool.parallelFor(context.Background(), 1, func(context.Context, int) {
		close(interactiveDone)
	})
	select {
	case <-interactiveDone:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("interactive analysis was blocked behind bulk work")
	}

	close(releaseBulk)
	select {
	case <-bulkDone:
	case <-time.After(time.Second):
		t.Fatal("bulk analysis did not finish")
	}
}

func TestBulkAnalysisUsesConfiguredCPUCapacityAndKeepsInteractiveSlot(t *testing.T) {
	pool := &analysisWorkerPool{}
	pool.setWorkers(4)
	started := make(chan struct{}, 8)
	release := make(chan struct{})
	done := make(chan struct{})
	var active atomic.Int64
	var maximum atomic.Int64
	go func() {
		defer close(done)
		pool.parallelForBulk(context.Background(), 8, func(context.Context, int) {
			current := active.Add(1)
			for {
				previous := maximum.Load()
				if current <= previous || maximum.CompareAndSwap(previous, current) {
					break
				}
			}
			started <- struct{}{}
			<-release
			active.Add(-1)
		})
	}()
	for range 4 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("bulk analysis did not fill reserved capacity")
		}
	}
	select {
	case <-started:
		t.Fatal("bulk analysis exceeded configured worker capacity")
	case <-time.After(50 * time.Millisecond):
	}

	interactiveDone := make(chan struct{})
	go pool.parallelFor(context.Background(), 1, func(context.Context, int) { close(interactiveDone) })
	select {
	case <-interactiveDone:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("interactive analysis was blocked behind bulk capacity")
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("bulk analysis did not finish")
	}
	if got := maximum.Load(); got != 4 {
		t.Fatalf("maximum concurrent bulk workers = %d, want 4", got)
	}
}

func TestNestedAnalysisBorrowsReleasedWorkerCapacityWithoutDeadlock(t *testing.T) {
	pool := &analysisWorkerPool{}
	pool.setWorkers(2)
	outerSiblingStarted := make(chan struct{})
	releaseOuterSibling := make(chan struct{})
	nestedStarted := make(chan struct{}, 3)
	releaseNested := make(chan struct{})
	completed := make(chan struct{})
	go func() {
		pool.parallelForBulk(context.Background(), 2, func(ctx context.Context, index int) {
			if index == 1 {
				close(outerSiblingStarted)
				<-releaseOuterSibling
				return
			}
			<-outerSiblingStarted
			pool.parallelForBulk(ctx, 3, func(context.Context, int) {
				nestedStarted <- struct{}{}
				<-releaseNested
			})
		})
		close(completed)
	}()
	select {
	case <-nestedStarted:
	case <-time.After(time.Second):
		t.Fatal("inline nested analysis did not start")
	}
	interactiveDone := make(chan struct{})
	go pool.parallelFor(context.Background(), 1, func(context.Context, int) { close(interactiveDone) })
	select {
	case <-interactiveDone:
	case <-time.After(250 * time.Millisecond):
		close(releaseOuterSibling)
		close(releaseNested)
		t.Fatal("nested bulk analysis consumed the editor-facing slot")
	}
	close(releaseOuterSibling)
	select {
	case <-nestedStarted:
	case <-time.After(time.Second):
		t.Fatal("nested analysis did not borrow released worker capacity")
	}
	close(releaseNested)
	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("nested analysis deadlocked while reacquiring worker slots")
	}
}

func TestNestedAnalysisCompletesWhenAllOuterWorkersNest(t *testing.T) {
	pool := &analysisWorkerPool{}
	pool.setWorkers(2)
	completed := make(chan struct{})
	go func() {
		pool.parallelForBulk(context.Background(), 2, func(ctx context.Context, _ int) {
			pool.parallelForBulk(ctx, 4, func(context.Context, int) {})
		})
		close(completed)
	}()
	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("mutually nested outer workers deadlocked")
	}
}

func TestRequestAnalysisFinishesWhileBulkWorkersAreSaturated(t *testing.T) {
	pool := &analysisWorkerPool{}
	pool.setWorkers(2)
	bulkStarted := make(chan struct{}, 2)
	releaseBulk := make(chan struct{})
	bulkDone := make(chan struct{})
	go func() {
		defer close(bulkDone)
		pool.parallelForBulk(context.Background(), 2, func(context.Context, int) {
			bulkStarted <- struct{}{}
			<-releaseBulk
		})
	}()
	for range 2 {
		select {
		case <-bulkStarted:
		case <-time.After(time.Second):
			t.Fatal("bulk analysis did not occupy every bulk slot")
		}
	}

	var completed atomic.Int64
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		pool.parallelForRequest(context.Background(), 5, func(context.Context, int) {
			completed.Add(1)
		})
	}()
	select {
	case <-requestDone:
	case <-time.After(time.Second):
		t.Fatal("request analysis waited behind saturated bulk work")
	}
	if completed.Load() != 5 {
		t.Fatalf("request analysis ran %d of 5 items", completed.Load())
	}

	bulkRequestDone := make(chan struct{})
	go func() {
		defer close(bulkRequestDone)
		pool.parallelForBulk(context.Background(), 1, func(context.Context, int) {})
	}()
	select {
	case <-bulkRequestDone:
		t.Fatal("bulk analysis did not wait for a bulk slot")
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseBulk)
	for _, done := range []chan struct{}{bulkDone, bulkRequestDone} {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("bulk analysis did not finish")
		}
	}
}
