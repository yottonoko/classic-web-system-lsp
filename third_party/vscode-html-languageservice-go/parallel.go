package htmlservice

import (
	"runtime"
	"sync"
)

func parallelMapOrdered[T any, R any](items []T, minItems int, fn func(int, T) R) []R {
	results := make([]R, len(items))
	if len(items) == 0 {
		return results
	}
	if len(items) < minItems || runtime.GOMAXPROCS(0) < 2 {
		for i, item := range items {
			results[i] = fn(i, item)
		}
		return results
	}
	workers := runtime.GOMAXPROCS(0)
	if workers > len(items) {
		workers = len(items)
	}
	jobs := make(chan int, workers)
	var wg sync.WaitGroup
	var relay panicRelay
	wg.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func() {
			defer wg.Done()
			// Recover per item so the worker keeps draining jobs; a dead
			// worker pool would block the producer below forever.
			for i := range jobs {
				func() {
					defer relay.capture()
					results[i] = fn(i, items[i])
				}()
			}
		}()
	}
	for i := range items {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	relay.rethrow()
	return results
}
