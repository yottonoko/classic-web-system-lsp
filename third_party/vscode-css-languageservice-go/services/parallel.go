package services

import (
	"runtime"
	"sync"
)

const (
	parallelTextThreshold           = 128 * 1024
	parallelBlockThreshold          = 512
	parallelBlockWorkTextThreshold  = 512 * 1024
	parallelBlockWorkBlockThreshold = 4096
)

func shouldParallelize(text string, blocks []cssBlock) bool {
	return len(text) >= parallelTextThreshold || len(blocks) >= parallelBlockThreshold
}

func shouldParallelizeBlockWork(text string, blocks []cssBlock) bool {
	return len(text) >= parallelBlockWorkTextThreshold || len(blocks) >= parallelBlockWorkBlockThreshold
}

func parallelWorkerCount(chunks int) int {
	if chunks <= 1 {
		return chunks
	}
	workers := runtime.GOMAXPROCS(0)
	if workers > chunks {
		workers = chunks
	}
	if workers < 1 {
		workers = 1
	}
	return workers
}

func parallelBlockMap[T any](blocks []cssBlock, workers int, fn func([]cssBlock) []T) []T {
	if len(blocks) == 0 {
		return nil
	}
	if workers <= 0 {
		workers = parallelWorkerCount(len(blocks))
	}
	if workers <= 1 || len(blocks) == 1 {
		return fn(blocks)
	}
	chunkSize := (len(blocks) + workers - 1) / workers
	results := make([][]T, workers)
	var wg sync.WaitGroup
	var relay panicRelay
	for worker := 0; worker < workers; worker++ {
		start := worker * chunkSize
		end := start + chunkSize
		if start >= len(blocks) {
			results = results[:worker]
			break
		}
		if end > len(blocks) {
			end = len(blocks)
		}
		chunk := blocks[start:end]
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			defer relay.capture()
			results[index] = fn(chunk)
		}(worker)
	}
	wg.Wait()
	relay.rethrow()
	var total int
	for _, values := range results {
		total += len(values)
	}
	merged := make([]T, 0, total)
	for _, values := range results {
		merged = append(merged, values...)
	}
	return merged
}
