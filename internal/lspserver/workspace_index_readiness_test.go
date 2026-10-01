package lspserver

import (
	"testing"
	"time"
)

func TestWorkspaceIndexItemProgressDueThrottlesAllButFinalUpdates(t *testing.T) {
	var last time.Time
	if !workspaceIndexItemProgressDue(&last, false) {
		t.Fatal("first progress update was throttled")
	}
	if workspaceIndexItemProgressDue(&last, false) {
		t.Fatal("immediate second progress update was not throttled")
	}
	if !workspaceIndexItemProgressDue(&last, true) {
		t.Fatal("final progress update was throttled")
	}
	last = time.Now().Add(-2 * workspaceIndexItemProgressInterval)
	if !workspaceIndexItemProgressDue(&last, false) {
		t.Fatal("progress update after the interval was throttled")
	}
}
