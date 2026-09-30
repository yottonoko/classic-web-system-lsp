package embedded

import (
	"fmt"
	"runtime/debug"
	"sync/atomic"
)

// PanicReporter receives failures recovered from the embedded language
// services. The HTML, CSS, and formatter ports are pure functions over their
// input, so one malformed document must not terminate the language server.
type PanicReporter func(operation string, recovered any, stack []byte)

var panicReporter atomic.Pointer[PanicReporter]

// SetPanicReporter installs the process-wide reporter; nil disables reporting.
func SetPanicReporter(reporter PanicReporter) {
	if reporter == nil {
		panicReporter.Store(nil)
		return
	}
	panicReporter.Store(&reporter)
}

func reportPanic(operation string, recovered any) {
	if reporter := panicReporter.Load(); reporter != nil {
		(*reporter)(operation, recovered, debug.Stack())
	}
}

// recoverService resets the named result to its zero value when the guarded
// service call panics.
func recoverService[T any](operation string, result *T) {
	if recovered := recover(); recovered != nil {
		reportPanic(operation, recovered)
		var zero T
		*result = zero
	}
}

// recoverFormat turns a formatter panic into an error so callers keep the
// original text.
func recoverFormat(operation string, text *string, err *error) {
	if recovered := recover(); recovered != nil {
		reportPanic(operation, recovered)
		*text = ""
		*err = fmt.Errorf("%s failed: %v", operation, recovered)
	}
}
