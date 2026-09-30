package lspserver

import (
	"sync"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

type parsedAnalysisOnceKey struct {
	parsed *core.ParsedDocument
	key    string
}

type parsedAnalysisFlight struct {
	done  chan struct{}
	value any
	ok    bool
}

var parsedAnalysisOnceFlights sync.Map

// singleflightParsedAnalysis runs compute once for concurrent callers asking
// for the same analysis of one parsed revision. compute must store its own
// result on parsed and must not request the same key again. A caller that
// waited on a build that panicked computes the value itself.
func singleflightParsedAnalysis[T any](parsed *core.ParsedDocument, key string, compute func() T) T {
	flightKey := parsedAnalysisOnceKey{parsed: parsed, key: key}
	pending := &parsedAnalysisFlight{done: make(chan struct{})}
	if actual, loaded := parsedAnalysisOnceFlights.LoadOrStore(flightKey, pending); loaded {
		existing := actual.(*parsedAnalysisFlight)
		<-existing.done
		if value, ok := existing.value.(T); ok && existing.ok {
			return value
		}
		return compute()
	}
	defer func() {
		parsedAnalysisOnceFlights.Delete(flightKey)
		close(pending.done)
	}()
	value := compute()
	pending.value, pending.ok = value, true
	return value
}
