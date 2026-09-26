package lspserver

import (
	"context"
	"io"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestDocumentOpenAnalysisBarrierIgnoresCanceledSaveWorker(t *testing.T) {
	fixture := newBlockedDocumentOpenAnalysisBarrierFixture(t)

	if err := fixture.server.handleNotification(context.Background(), "textDocument/didSave", mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": fixture.uri},
	})); err != nil {
		t.Fatal(err)
	}
	fixture.assertCanceledTombstone(t)
	fixture.assertBarrierReturns(t)
	fixture.release()
	fixture.waitForWorker(t)
}

func TestDocumentOpenAnalysisBarrierIgnoresCanceledCloseWorker(t *testing.T) {
	fixture := newBlockedDocumentOpenAnalysisBarrierFixture(t)

	if err := fixture.server.handleNotification(context.Background(), "textDocument/didClose", mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": fixture.uri},
	})); err != nil {
		t.Fatal(err)
	}
	fixture.assertCanceledTombstone(t)
	fixture.assertBarrierReturns(t)
	fixture.release()
	fixture.waitForWorker(t)
}

func TestDocumentOpenAnalysisBarrierWaitsForActiveURIWithoutCanceledWorker(t *testing.T) {
	root := t.TempDir()
	server := New(nil, io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = pathToFileURI(root)
	server.settings.CacheEnabled = false
	server.configureDiskAnalysisCache()
	uriA := pathToFileURI(filepath.Join(root, "canceled.asp"))
	uriB := pathToFileURI(filepath.Join(root, "active.asp"))
	aStarted := make(chan struct{})
	aRelease := make(chan struct{})
	aFinished := make(chan struct{})
	bStarted := make(chan struct{})
	bRelease := make(chan struct{})
	bFinished := make(chan struct{})
	var aReleaseOnce sync.Once
	var bReleaseOnce sync.Once
	var builds atomic.Int32
	server.fileAnalysisSnapshotTestHook = func() {
		switch builds.Add(1) {
		case 1:
			close(aStarted)
			<-aRelease
			close(aFinished)
		case 2:
			close(bStarted)
			<-bRelease
			close(bFinished)
		}
	}
	releaseA := func() { aReleaseOnce.Do(func() { close(aRelease) }) }
	releaseB := func() { bReleaseOnce.Do(func() { close(bRelease) }) }
	t.Cleanup(func() {
		releaseA()
		releaseB()
		server.cancelDocumentOpenAnalysis(uriA)
		server.cancelDocumentOpenAnalysis(uriB)
		server.stopDocumentOpenAnalysisWorkers()
		server.shutdownRuntimeCaches()
	})

	documentA := core.NewTextDocument(uriA, "classic-asp", 1, "<% Dim canceledValue : canceledValue = 1 %>")
	documentB := core.NewTextDocument(uriB, "classic-asp", 1, "<% Dim activeValue : activeValue = 1 %>")
	parsedA := core.ParseDocument(uriA, documentA.Text, core.Settings{DefaultLanguage: server.settings.DefaultLanguage})
	parsedB := core.ParseDocument(uriB, documentB.Text, core.Settings{DefaultLanguage: server.settings.DefaultLanguage})
	server.mu.Lock()
	server.rememberOpenDocumentLocked(uriA, documentA)
	server.rememberOpenDocumentLocked(uriB, documentB)
	server.mu.Unlock()
	server.scheduleDocumentChangeAnalysis(documentA, parsedA)
	waitForDocumentOpenBarrierChannel(t, aStarted, "URI A analysis did not start")
	server.scheduleDocumentChangeAnalysis(documentB, parsedB)
	waitForDocumentOpenBarrierChannel(t, bStarted, "URI B analysis did not start")
	server.cancelDocumentOpenAnalysis(uriA)

	baseContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	barrierContext := &releaseOnDoneDocumentOpenAnalysisContext{Context: baseContext, release: releaseB}
	result := make(chan bool, 1)
	go func() { result <- server.waitForDocumentOpenAnalysisContext(barrierContext) }()
	select {
	case got := <-result:
		if !got {
			t.Fatal("document-open analysis barrier did not complete active URI B")
		}
	case <-baseContext.Done():
		t.Fatal("document-open analysis barrier waited for canceled URI A")
	}
	select {
	case <-bFinished:
	default:
		t.Fatal("document-open analysis barrier returned before active URI B completed")
	}
	select {
	case <-aFinished:
		t.Fatal("canceled URI A worker completed before its release")
	default:
	}
	releaseA()
}

type releaseOnDoneDocumentOpenAnalysisContext struct {
	context.Context
	releaseOnce sync.Once
	release     func()
}

func (c *releaseOnDoneDocumentOpenAnalysisContext) Done() <-chan struct{} {
	c.releaseOnce.Do(c.release)
	return c.Context.Done()
}

func waitForDocumentOpenBarrierChannel(t *testing.T, channel <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-channel:
	case <-time.After(2 * time.Second):
		t.Fatal(message)
	}
}

type blockedDocumentOpenAnalysisBarrierFixture struct {
	server      *Server
	uri         string
	releaseCh   chan struct{}
	releaseOnce sync.Once
	workerDone  chan struct{}
}

func newBlockedDocumentOpenAnalysisBarrierFixture(t *testing.T) *blockedDocumentOpenAnalysisBarrierFixture {
	t.Helper()
	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "barrier.asp"))
	server := New(nil, io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = pathToFileURI(root)
	server.settings.CacheEnabled = false
	server.configureDiskAnalysisCache()
	started := make(chan struct{})
	releaseCh := make(chan struct{})
	var startedOnce sync.Once
	fixture := &blockedDocumentOpenAnalysisBarrierFixture{
		server: server, uri: uri, releaseCh: releaseCh,
	}
	t.Cleanup(func() {
		fixture.release()
		server.cancelDocumentOpenAnalysis(uri)
		server.cancelScheduledDiagnostics(uri)
		server.stopDocumentOpenAnalysisWorkers()
		server.shutdownRuntimeCaches()
	})

	server.fileAnalysisSnapshotTestHook = func() {
		blocked := false
		startedOnce.Do(func() {
			blocked = true
			close(started)
		})
		if blocked {
			<-releaseCh
		}
	}

	document := core.NewTextDocument(uri, "classic-asp", 1, "<% Dim value : value = 1 %>")
	parsed := core.ParseDocument(uri, document.Text, core.Settings{DefaultLanguage: server.settings.DefaultLanguage})
	server.mu.Lock()
	server.rememberOpenDocumentLocked(uri, document)
	server.mu.Unlock()
	server.scheduleDocumentChangeAnalysis(document, parsed)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("document-open analysis did not reach the blocked phase")
	}

	fixture.workerDone = make(chan struct{})
	go func() {
		server.documentOpenAnalysisWorkers.Wait()
		close(fixture.workerDone)
	}()
	return fixture
}

func (f *blockedDocumentOpenAnalysisBarrierFixture) assertCanceledTombstone(t *testing.T) {
	t.Helper()
	f.server.mu.Lock()
	job := f.server.documentOpenAnalysisJobs[diagnosticTimerKey(f.uri)]
	active := job != nil && job.active
	f.server.mu.Unlock()
	if job == nil || active {
		t.Fatalf("document-open analysis job present = %t, active = %t; want inactive tombstone", job != nil, active)
	}
	select {
	case <-f.workerDone:
		t.Fatal("canceled document-open analysis worker exited before release")
	default:
	}
}

func (f *blockedDocumentOpenAnalysisBarrierFixture) assertBarrierReturns(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan bool, 1)
	go func() { result <- f.server.waitForDocumentOpenAnalysisContext(ctx) }()
	select {
	case got := <-result:
		if !got {
			t.Fatal("document-open analysis barrier returned false for a canceled worker")
		}
	case <-ctx.Done():
		t.Fatal("document-open analysis barrier waited for a canceled worker")
	}
}

func (f *blockedDocumentOpenAnalysisBarrierFixture) release() {
	f.releaseOnce.Do(func() { close(f.releaseCh) })
}

func (f *blockedDocumentOpenAnalysisBarrierFixture) waitForWorker(t *testing.T) {
	t.Helper()
	select {
	case <-f.workerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("document-open analysis worker did not stop after release")
	}
}
