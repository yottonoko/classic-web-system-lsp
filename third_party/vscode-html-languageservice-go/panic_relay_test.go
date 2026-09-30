package htmlservice

import (
	"runtime"
	"testing"
)

func TestParallelMapOrderedRethrowsWorkerPanicOnCaller(t *testing.T) {
	if runtime.GOMAXPROCS(0) < 2 {
		t.Skip("needs parallel workers")
	}
	items := make([]int, 256)
	recovered := func() (recovered any) {
		defer func() { recovered = recover() }()
		parallelMapOrdered(items, 1, func(index int, _ int) int {
			if index%7 == 3 {
				panic("worker failure")
			}
			return index
		})
		return nil
	}()
	if recovered != "worker failure" {
		t.Fatalf("recovered = %v, want the worker panic", recovered)
	}
}
