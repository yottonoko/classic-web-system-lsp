package services

import "sync"

// panicRelay moves a panic from worker goroutines back to the goroutine that
// waits for them. A panic on a worker cannot be recovered by the caller and
// would otherwise terminate the whole process.
type panicRelay struct {
	once  sync.Once
	value any
	set   bool
}

// capture must be deferred on the worker before its WaitGroup is released.
func (r *panicRelay) capture() {
	if recovered := recover(); recovered != nil {
		r.once.Do(func() {
			r.value = recovered
			r.set = true
		})
	}
}

// rethrow must be called after every worker has finished.
func (r *panicRelay) rethrow() {
	if r.set {
		panic(r.value)
	}
}
