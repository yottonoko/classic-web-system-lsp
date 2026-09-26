package lspserver

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
)

type analysisWorkerPool struct {
	mu        sync.RWMutex
	workers   int
	slots     chan struct{}
	bulkSlots chan struct{}
}

const maxAnalysisWorkers = 256

type analysisWorkerContextKey struct{}

func (p *analysisWorkerPool) setWorkers(workers int) {
	workers = boundedAnalysisWorkers(workers)
	p.mu.Lock()
	p.workers = workers
	p.slots = make(chan struct{}, workers+1)
	p.bulkSlots = make(chan struct{}, workers)
	p.mu.Unlock()
}

func (p *analysisWorkerPool) workerCount() int {
	if p == nil {
		return 1
	}
	p.mu.RLock()
	workers := p.workers
	p.mu.RUnlock()
	return workers
}

func newAnalysisWorkerPoolFromEnv() *analysisWorkerPool {
	workers := defaultAnalysisWorkers()
	if raw := os.Getenv("ASP_LSP_ANALYSIS_WORKERS"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			workers = parsed
		}
	}
	workers = boundedAnalysisWorkers(workers)
	return &analysisWorkerPool{workers: workers, slots: make(chan struct{}, workers+1), bulkSlots: make(chan struct{}, workers)}
}

func defaultAnalysisWorkers() int {
	return boundedAnalysisWorkers(runtime.NumCPU())
}

func boundedAnalysisWorkers(workers int) int {
	if workers < 1 {
		return 1
	}
	if workers > maxAnalysisWorkers {
		return maxAnalysisWorkers
	}
	return workers
}

func (s *Server) configureAnalysisWorkers() {
	s.mu.Lock()
	requested := s.settings.WorkspaceBusyAnalysisConcurrency
	pool := s.analysisWorkers
	s.mu.Unlock()
	if pool == nil {
		return
	}
	if requested == 0 {
		if raw := os.Getenv("ASP_LSP_ANALYSIS_WORKERS"); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil {
				requested = parsed
				if requested < 1 {
					requested = 1
				}
			}
		}
		if requested == 0 {
			requested = defaultAnalysisWorkers()
		}
	}
	pool.setWorkers(requested)
}

func (p *analysisWorkerPool) parallelFor(ctx context.Context, count int, work func(context.Context, int)) {
	p.parallelForClass(ctx, count, false, work)
}

// parallelForBulk shares the configured CPU-sized limiter across all bulk
// callers. The global limiter has one additional editor-facing slot.
func (p *analysisWorkerPool) parallelForBulk(ctx context.Context, count int, work func(context.Context, int)) {
	p.parallelForClass(ctx, count, true, work)
}

func (p *analysisWorkerPool) parallelForClass(ctx context.Context, count int, bulk bool, work func(context.Context, int)) {
	if count <= 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	workers := 1
	if p != nil && p.workerCount() > 1 {
		workers = p.workerCount()
	}
	nested := ctx.Value(analysisWorkerContextKey{}) != nil
	if workers > count {
		workers = count
	}
	p.mu.RLock()
	slots := p.slots
	bulkSlots := p.bulkSlots
	p.mu.RUnlock()
	parentBulk, _ := ctx.Value(analysisWorkerContextKey{}).(bool)
	nestedBulkSlots := bulkSlots
	if nested {
		// The calling worker already owns its slots. Nested bulk helpers borrow
		// capacity released by sibling outer tasks while preserving the spare
		// editor-facing slot. Waiting helpers are released when the inline worker
		// drains the queue, avoiding a cycle when every outer worker nests.
		bulkSlots = nil
		if !parentBulk {
			nestedBulkSlots = nil
		}
	}
	if !bulk {
		bulkSlots = nil
	}
	acquire := func() bool {
		if bulkSlots != nil {
			select {
			case bulkSlots <- struct{}{}:
			case <-ctx.Done():
				return false
			}
		}
		if slots != nil {
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				if bulkSlots != nil {
					<-bulkSlots
				}
				return false
			}
		}
		return true
	}
	release := func() {
		if slots != nil {
			<-slots
		}
		if bulkSlots != nil {
			<-bulkSlots
		}
	}
	workerCtx := context.WithValue(ctx, analysisWorkerContextKey{}, bulk || parentBulk)
	if nested {
		if workers <= 1 || slots == nil {
			for i := 0; i < count; i++ {
				if ctx.Err() != nil {
					return
				}
				work(workerCtx, i)
			}
			return
		}
		var next atomic.Int64
		done := make(chan struct{})
		var doneOnce sync.Once
		finish := func() { doneOnce.Do(func() { close(done) }) }
		var wg sync.WaitGroup
		wg.Add(workers - 1)
		for worker := 1; worker < workers; worker++ {
			go func() {
				defer wg.Done()
				bulkAcquired := false
				if nestedBulkSlots != nil {
					select {
					case nestedBulkSlots <- struct{}{}:
						bulkAcquired = true
					case <-done:
						return
					case <-ctx.Done():
						return
					}
				}
				select {
				case slots <- struct{}{}:
					defer func() {
						<-slots
						if bulkAcquired {
							<-nestedBulkSlots
						}
					}()
				case <-done:
					if bulkAcquired {
						<-nestedBulkSlots
					}
					return
				case <-ctx.Done():
					if bulkAcquired {
						<-nestedBulkSlots
					}
					return
				}
				for {
					index := int(next.Add(1) - 1)
					if index >= count || ctx.Err() != nil {
						return
					}
					work(workerCtx, index)
				}
			}()
		}
		for {
			index := int(next.Add(1) - 1)
			if index >= count || ctx.Err() != nil {
				break
			}
			work(workerCtx, index)
		}
		finish()
		wg.Wait()
		return
	}
	if workers <= 1 {
		if !acquire() {
			return
		}
		defer release()
		for i := 0; i < count; i++ {
			if ctx.Err() != nil {
				return
			}
			work(workerCtx, i)
		}
		return
	}
	if bulkSlots != nil && workers > cap(bulkSlots) {
		workers = cap(bulkSlots)
	}
	var next atomic.Int64
	var wg sync.WaitGroup
	wg.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func() {
			defer wg.Done()
			if !acquire() {
				return
			}
			defer release()
			for {
				index := int(next.Add(1) - 1)
				if index >= count || ctx.Err() != nil {
					return
				}
				work(workerCtx, index)
			}
		}()
	}
	wg.Wait()
}
