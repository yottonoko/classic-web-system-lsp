package core

import (
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type runtimeAnalysisReleaseValue struct {
	name string
}

func TestLoadOrStoreRuntimeAnalysisKeepsPredecessorUntilInitializationCompletes(t *testing.T) {
	const key = "test.runtime-analysis.release"
	previous := ParseDocument("file:///runtime-analysis-release.asp", "<% Dim before %>", Settings{DefaultLanguage: "VBScript"})
	previousValue := &runtimeAnalysisReleaseValue{name: "before"}
	previous.StoreRuntimeAnalysis(key, previousValue)

	updated := ParseDocument(previous.URI, "<% Dim after %>", Settings{DefaultLanguage: "VBScript"})
	updated.inheritPreviousRevision(previous)

	inherited, ok := updated.LoadPreviousRuntimeAnalysis(key)
	if !ok || inherited != previousValue {
		t.Fatalf("inherited runtime value = %#v, present=%t; want %#v", inherited, ok, previousValue)
	}

	// A factory may inspect and reuse the inherited value before publishing its
	// initialized replacement.
	candidate := &runtimeAnalysisReleaseValue{name: inherited.(*runtimeAnalysisReleaseValue).name}
	actual, loaded := updated.LoadOrStoreRuntimeAnalysis(key, candidate)
	if loaded || actual != candidate {
		t.Fatalf("LoadOrStore result = (%#v, %t), want candidate and loaded=false", actual, loaded)
	}
	if inherited, ok := updated.LoadPreviousRuntimeAnalysis(key); !ok || inherited != previousValue {
		t.Fatalf("predecessor after candidate install = %#v, present=%t; want factory value retained", inherited, ok)
	}
	if !updated.ReleasePreviousRuntimeAnalysis(key, actual) {
		t.Fatal("ReleasePreviousRuntimeAnalysis did not release the initialized predecessor")
	}
	if _, ok := updated.LoadPreviousRuntimeAnalysis(key); ok {
		t.Fatal("predecessor remained after initialized value was released")
	}
	if current, ok := updated.LoadRuntimeAnalysis(key); !ok || current != candidate {
		t.Fatalf("current runtime value = %#v, present=%t; want candidate", current, ok)
	}
}

func TestLoadOrStoreRuntimeAnalysisConcurrentCallersShareOneCurrentValue(t *testing.T) {
	const (
		key     = "test.runtime-analysis.concurrent-release"
		workers = 64
	)
	previous := ParseDocument("file:///runtime-analysis-concurrent.asp", "<% Dim before %>", Settings{DefaultLanguage: "VBScript"})
	previousValue := &runtimeAnalysisReleaseValue{name: "before"}
	previous.StoreRuntimeAnalysis(key, previousValue)
	updated := ParseDocument(previous.URI, "<% Dim after %>", Settings{DefaultLanguage: "VBScript"})
	updated.inheritPreviousRevision(previous)

	start := make(chan struct{})
	var complete sync.WaitGroup
	complete.Add(workers)
	var installed atomic.Int64
	var first atomic.Pointer[runtimeAnalysisReleaseValue]
	for range workers {
		go func() {
			defer complete.Done()
			<-start
			candidate := &runtimeAnalysisReleaseValue{name: "current"}
			actual, loaded := updated.LoadOrStoreRuntimeAnalysis(key, candidate)
			current, ok := actual.(*runtimeAnalysisReleaseValue)
			if !ok || current == nil {
				t.Errorf("current runtime value = %#v, want *runtimeAnalysisReleaseValue", actual)
				return
			}
			if !loaded {
				installed.Add(1)
			}
			first.CompareAndSwap(nil, current)
		}()
	}
	close(start)
	complete.Wait()

	if got := installed.Load(); got != 1 {
		t.Fatalf("successful current installs = %d, want 1", got)
	}
	current, ok := updated.LoadRuntimeAnalysis(key)
	if !ok || current != first.Load() {
		t.Fatalf("current runtime value = %#v, present=%t; want one shared value %#v", current, ok, first.Load())
	}
	if _, ok := updated.LoadPreviousRuntimeAnalysis(key); !ok {
		t.Fatal("predecessor was released before concurrent initialization completed")
	}
	if !updated.ReleasePreviousRuntimeAnalysis(key, current) {
		t.Fatal("concurrent current value could not release its predecessor")
	}
}

func TestLoadOrStoreRuntimeAnalysisReusesSharedPredecessorBacking(t *testing.T) {
	const key = "test.runtime-analysis.shared-backing"
	previous := ParseDocument("file:///runtime-analysis-shared.asp", "<% Dim before %>", Settings{DefaultLanguage: "VBScript"})
	shared := []byte("shared runtime")
	previous.StoreRuntimeAnalysis(key, shared)
	updated := ParseDocument(previous.URI, "<% Dim after %>", Settings{DefaultLanguage: "VBScript"})
	updated.inheritPreviousRevision(previous)

	actual, loaded := updated.LoadOrStoreRuntimeAnalysis(key, shared)
	if loaded || actual == nil {
		t.Fatalf("shared LoadOrStore result = (%#v, %t), want an installed value", actual, loaded)
	}
	if !sameRuntimeAnalysisBacking(actual, shared) {
		t.Fatal("current value did not retain the predecessor backing")
	}
	previousOwners := ownerBytesByIdentity(previous.RuntimeAnalysisMemoryOwners())
	updatedOwners := ownerBytesByIdentity(updated.RuntimeAnalysisMemoryOwners())
	if len(previousOwners) != 1 || len(updatedOwners) != 1 {
		t.Fatalf("shared runtime owners = previous %#v updated %#v, want one owner each", previousOwners, updatedOwners)
	}
	for identity, bytes := range previousOwners {
		if updatedOwners[identity] != bytes {
			t.Fatalf("shared owner %v bytes = previous(%d) updated(%d), want equal", identity, bytes, bytes)
		}
	}
	if !updated.ReleasePreviousRuntimeAnalysis(key, actual) {
		t.Fatal("shared current value could not release predecessor")
	}
}

func TestReleasePreviousRuntimeAnalysisRejectsFailedOrStaleInitialization(t *testing.T) {
	const key = "test.runtime-analysis.failed-release"
	previous := ParseDocument("file:///runtime-analysis-failed.asp", "<% Dim before %>", Settings{DefaultLanguage: "VBScript"})
	previousValue := &runtimeAnalysisReleaseValue{name: "before"}
	previous.StoreRuntimeAnalysis(key, previousValue)
	updated := ParseDocument(previous.URI, "<% Dim after %>", Settings{DefaultLanguage: "VBScript"})
	updated.inheritPreviousRevision(previous)

	if actual, loaded := updated.LoadOrStoreRuntimeAnalysis(key, nil); actual != nil || loaded {
		t.Fatalf("nil LoadOrStore result = (%#v, %t), want (nil, false)", actual, loaded)
	}
	if _, ok := updated.LoadPreviousRuntimeAnalysis(key); !ok {
		t.Fatal("nil factory result released predecessor")
	}
	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Fatal("panic factory did not panic")
			}
		}()
		panic("factory failure")
	}()
	if _, ok := updated.LoadPreviousRuntimeAnalysis(key); !ok {
		t.Fatal("panic factory released predecessor")
	}

	candidate := &runtimeAnalysisReleaseValue{name: "current"}
	actual, loaded := updated.LoadOrStoreRuntimeAnalysis(key, candidate)
	if loaded || actual != candidate {
		t.Fatalf("candidate install result = (%#v, %t), want candidate and loaded=false", actual, loaded)
	}
	stale := &runtimeAnalysisReleaseValue{name: "stale"}
	if updated.ReleasePreviousRuntimeAnalysis(key, stale) {
		t.Fatal("stale initializer released predecessor")
	}
	if _, ok := updated.LoadPreviousRuntimeAnalysis(key); !ok {
		t.Fatal("predecessor was released by failed initialization")
	}
	if !updated.ReleasePreviousRuntimeAnalysis(key, actual) {
		t.Fatal("successful initializer could not release predecessor")
	}
}

func TestReleasePreviousRuntimeAnalysisRejectsDynamicallyUncomparableExpectedValue(t *testing.T) {
	const key = "test.runtime-analysis.uncomparable-release"
	previous := ParseDocument("file:///runtime-analysis-uncomparable.asp", "<% Dim before %>", Settings{DefaultLanguage: "VBScript"})
	previous.StoreRuntimeAnalysis(key, &runtimeAnalysisReleaseValue{name: "before"})
	updated := ParseDocument(previous.URI, "<% Dim after %>", Settings{DefaultLanguage: "VBScript"})
	updated.inheritPreviousRevision(previous)

	type wrappedValue struct {
		value any
	}
	actual, loaded := updated.LoadOrStoreRuntimeAnalysis(key, wrappedValue{value: map[string]string{"state": "current"}})
	if loaded || actual == nil {
		t.Fatalf("current uncomparable value = (%#v, %t), want installed value", actual, loaded)
	}
	if updated.ReleasePreviousRuntimeAnalysis(key, wrappedValue{value: map[string]string{"state": "stale"}}) {
		t.Fatal("dynamically uncomparable stale value released predecessor")
	}
	if _, ok := updated.LoadPreviousRuntimeAnalysis(key); !ok {
		t.Fatal("dynamically uncomparable comparison released predecessor")
	}
}

type runtimeAnalysisRevisionSpecificValue struct{}

func (runtimeAnalysisRevisionSpecificValue) SkipPreviousRuntimeInheritance() {}

func TestLoadOrStoreRuntimeAnalysisDoesNotReleaseSkippedInheritance(t *testing.T) {
	const key = "test.runtime-analysis.skipped"
	previous := ParseDocument("file:///runtime-analysis-skipped.asp", "<% Dim before %>", Settings{DefaultLanguage: "VBScript"})
	previous.StoreRuntimeAnalysis(key, runtimeAnalysisRevisionSpecificValue{})
	updated := ParseDocument(previous.URI, "<% Dim after %>", Settings{DefaultLanguage: "VBScript"})
	updated.inheritPreviousRevision(previous)
	if _, ok := updated.LoadPreviousRuntimeAnalysis(key); ok {
		t.Fatal("revision-specific runtime value was inherited")
	}
	actual, loaded := updated.LoadOrStoreRuntimeAnalysis(key, runtimeAnalysisRevisionSpecificValue{})
	if loaded || actual == nil {
		t.Fatalf("current skipped runtime value = (%#v, %t), want installed value", actual, loaded)
	}
	if updated.ReleasePreviousRuntimeAnalysis(key, actual) {
		t.Fatal("release reported a skipped predecessor")
	}
}

func TestParsedDocumentAnalysisLockIsPerDocumentAndSharedByCopies(t *testing.T) {
	first := ParseDocument("file:///analysis-lock-first.asp", "<% Dim first %>", Settings{DefaultLanguage: "VBScript"})
	second := ParseDocument("file:///analysis-lock-second.asp", "<% Dim second %>", Settings{DefaultLanguage: "VBScript"})
	first.StoreRuntimeAnalysis("test.lock", "first")
	copied := *first
	if copied.analysisLock() != first.analysisLock() {
		t.Fatal("value copy made after storing analysis does not share the analysis lock")
	}
	if first.analysisLock() == second.analysisLock() {
		t.Fatal("distinct documents share one analysis lock")
	}

	// Holding one document's lock must not block analysis on another document.
	first.analysisLock().Lock()
	done := make(chan struct{})
	go func() {
		second.StoreRuntimeAnalysis("test.lock", "second")
		close(done)
	}()
	<-done
	first.analysisLock().Unlock()
	if value, ok := second.LoadRuntimeAnalysis("test.lock"); !ok || value != "second" {
		t.Fatalf("second runtime analysis = %#v, present=%t", value, ok)
	}
}

func TestParsedDocumentAnalysisLockAllowsConcurrentInheritance(t *testing.T) {
	previous := ParseDocument("file:///analysis-lock-inherit.asp", "<% Dim before %>", Settings{DefaultLanguage: "VBScript"})
	previous.StoreRuntimeAnalysis("test.inherit", "before")
	var group sync.WaitGroup
	for index := range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			updated := ParseDocument(previous.URI, "<% Dim after %>", Settings{DefaultLanguage: "VBScript"})
			updated.inheritPreviousRevision(previous)
			previous.StoreRuntimeAnalysis("test.concurrent", index)
			if value, ok := updated.LoadPreviousRuntimeAnalysis("test.inherit"); !ok || value != "before" {
				t.Errorf("inherited runtime analysis = %#v, present=%t", value, ok)
			}
		}()
	}
	group.Wait()
}

func TestExportedMemoryOwnerIdentityComparesBackings(t *testing.T) {
	text := strings.Repeat("owner", 8)
	backing, ok := runtimeValueBackingIdentity(reflect.ValueOf(text))
	if !ok {
		t.Fatal("string has no backing identity")
	}
	same, _ := runtimeValueBackingIdentity(reflect.ValueOf(text))
	if exportedMemoryOwnerIdentity(backing) != exportedMemoryOwnerIdentity(same) {
		t.Fatal("equal backings exported different identities")
	}
	bytes := []byte(text)
	other, _ := runtimeValueBackingIdentity(reflect.ValueOf(bytes))
	if exportedMemoryOwnerIdentity(backing) == exportedMemoryOwnerIdentity(other) {
		t.Fatal("distinct backings exported equal identities")
	}
	if got := exportedMemoryOwnerIdentity("plain"); got != "plain" {
		t.Fatalf("non-backing identity = %#v, want it unchanged", got)
	}
}
