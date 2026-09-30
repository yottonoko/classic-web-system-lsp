package services

import "testing"

func TestParallelBlockMapRethrowsWorkerPanicOnCaller(t *testing.T) {
	blocks := make([]cssBlock, 64)
	recovered := func() (recovered any) {
		defer func() { recovered = recover() }()
		parallelBlockMap(blocks, 4, func(chunk []cssBlock) []int {
			panic("worker failure")
		})
		return nil
	}()
	if recovered != "worker failure" {
		t.Fatalf("recovered = %v, want the worker panic", recovered)
	}
}
