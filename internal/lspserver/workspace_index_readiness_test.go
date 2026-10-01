package lspserver

import (
	"context"
	"io"
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

func TestWaitForCompleteWorkspaceReferenceIndexReturnsOnReadinessBeforeWorkerFinishes(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	done := make(chan struct{})
	server.mu.Lock()
	server.workspaceIndexEnabled = true
	server.workspaceIndexGeneration = 3
	server.workspaceIndexDone = done
	server.workspaceReferenceIndexReadySignal = make(chan struct{})
	server.mu.Unlock()

	result := make(chan bool, 1)
	go func() { result <- server.waitForCompleteWorkspaceReferenceIndex(context.Background()) }()
	select {
	case ready := <-result:
		t.Fatalf("wait returned %t before readiness", ready)
	case <-time.After(20 * time.Millisecond):
	}

	server.mu.Lock()
	server.workspaceReferenceIndexReadyGeneration = 3
	server.signalWorkspaceReferenceIndexReadyLocked()
	server.signalWorkspaceReferenceIndexReadyLocked()
	server.mu.Unlock()
	select {
	case ready := <-result:
		if !ready {
			t.Fatal("wait reported an incomplete index after readiness was signalled")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wait did not return after readiness was signalled while the worker was still running")
	}
	close(done)
}

func TestWaitForCompleteWorkspaceReferenceIndexReportsIncompleteWorker(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	done := make(chan struct{})
	server.mu.Lock()
	server.workspaceIndexEnabled = true
	server.workspaceIndexGeneration = 2
	server.workspaceIndexDone = done
	server.workspaceReferenceIndexReadySignal = make(chan struct{})
	server.mu.Unlock()
	close(done)
	if server.waitForCompleteWorkspaceReferenceIndex(context.Background()) {
		t.Fatal("wait reported readiness for a worker that finished without publishing it")
	}
}
