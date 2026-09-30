package core

import (
	"sync"
	"testing"
)

func TestPanicRelayRethrowsWorkerPanicOnCaller(t *testing.T) {
	var relay panicRelay
	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer relay.capture()
			panic("worker failure")
		}()
	}
	wg.Wait()
	recovered := func() (recovered any) {
		defer func() { recovered = recover() }()
		relay.rethrow()
		return nil
	}()
	if recovered != "worker failure" {
		t.Fatalf("recovered = %v, want the worker panic", recovered)
	}

	var quiet panicRelay
	func() {
		defer quiet.capture()
	}()
	quiet.rethrow()
}
