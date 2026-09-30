package lspserver

import (
	"fmt"
	"sync"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/embedded"
)

const embeddedPanicWarningInterval = time.Minute

// embeddedPanicReporter logs failures recovered from the embedded language
// services. Diagnostics rerun on every edit, so client warnings are limited
// to one per operation per interval; the debug log keeps every stack.
type embeddedPanicReporter struct {
	mu         sync.Mutex
	lastWarned map[string]time.Time
}

func (s *Server) installEmbeddedPanicReporter() func() {
	reporter := &embeddedPanicReporter{lastWarned: map[string]time.Time{}}
	embedded.SetPanicReporter(func(operation string, recovered any, stack []byte) {
		message := fmt.Sprintf("[asp-lsp] Embedded language service %s failed and was skipped: %v", operation, recovered)
		s.logDebugFileWithMetadata("ERROR", "embedded.panic", message, map[string]any{"operation": operation, "stack": string(stack)})
		if reporter.shouldWarn(operation, time.Now()) {
			s.logServerWarning(message)
		}
	})
	return func() { embedded.SetPanicReporter(nil) }
}

func (r *embeddedPanicReporter) shouldWarn(operation string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if last, ok := r.lastWarned[operation]; ok && now.Sub(last) < embeddedPanicWarningInterval {
		return false
	}
	r.lastWarned[operation] = now
	return true
}
