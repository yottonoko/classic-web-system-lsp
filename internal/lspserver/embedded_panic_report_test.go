package lspserver

import (
	"testing"
	"time"
)

func TestEmbeddedPanicReporterLimitsWarningsPerOperation(t *testing.T) {
	reporter := &embeddedPanicReporter{lastWarned: map[string]time.Time{}}
	start := time.Unix(1000, 0)
	if !reporter.shouldWarn("html.Hover", start) {
		t.Fatal("first failure should warn")
	}
	if reporter.shouldWarn("html.Hover", start.Add(embeddedPanicWarningInterval/2)) {
		t.Fatal("repeated failure inside the interval should stay quiet")
	}
	if !reporter.shouldWarn("css.Hover", start.Add(time.Second)) {
		t.Fatal("a different operation should warn")
	}
	if !reporter.shouldWarn("html.Hover", start.Add(embeddedPanicWarningInterval)) {
		t.Fatal("failure after the interval should warn again")
	}
}
