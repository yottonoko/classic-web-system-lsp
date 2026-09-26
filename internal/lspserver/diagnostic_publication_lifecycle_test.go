package lspserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func TestDiagnosticPublicationClosePreservesOtherOwnerContributions(t *testing.T) {
	var output bytes.Buffer
	server := New(strings.NewReader(""), &output, io.Discard)
	childURI := "file:///tmp/shared-diagnostics-child.inc"
	ownerAURI := "file:///tmp/shared-diagnostics-a.asp"
	ownerBURI := "file:///tmp/shared-diagnostics-b.asp"

	childDiagnostic := publicationTestDiagnostic(childURI, "child-owner")
	ownerADiagnostic := publicationTestDiagnostic(childURI, "owner-a")
	ownerBDiagnostic := publicationTestDiagnostic(childURI, "owner-b")
	server.updatePublishedDiagnosticContributions(ownerAURI, map[string][]lsp.Diagnostic{childURI: {ownerADiagnostic}})
	server.updatePublishedDiagnosticContributions(ownerBURI, map[string][]lsp.Diagnostic{childURI: {ownerBDiagnostic}})
	server.updatePublishedDiagnosticContributions(childURI, map[string][]lsp.Diagnostic{childURI: {childDiagnostic}})

	if err := server.clearPublishedDiagnosticTargetsForOwner(childURI); err != nil {
		t.Fatal(err)
	}
	messages := publishedDiagnosticMessages(t, output.Bytes())
	if len(messages) != 1 || len(messages[0]) != 2 {
		t.Fatalf("child close publication = %#v, want the two open-owner diagnostics", messages)
	}
	got := diagnosticMessages(messages[0])
	if got[0] != "owner-a" || got[1] != "owner-b" {
		t.Fatalf("child close diagnostics = %#v, want owner-a then owner-b", got)
	}

	output.Reset()
	if err := server.clearPublishedDiagnosticTargetsForOwner(ownerAURI); err != nil {
		t.Fatal(err)
	}
	messages = publishedDiagnosticMessages(t, output.Bytes())
	if len(messages) != 2 {
		t.Fatalf("owner close publications = %#v, want owner clear and child aggregate", messages)
	}
	if len(messages[0]) != 0 || len(messages[1]) != 1 || diagnosticMessages(messages[1])[0] != "owner-b" {
		t.Fatalf("owner close aggregate = %#v, want only owner-b on child", messages)
	}
}

func TestDiagnosticPublicationSerializesSharedChildPlans(t *testing.T) {
	root := t.TempDir()
	childPath := filepath.Join(root, "shared.inc")
	ownerAPath := filepath.Join(root, "a.asp")
	ownerBPath := filepath.Join(root, "b.asp")
	childSource := "<% shared = 1 %>"
	ownerSource := "<!-- #include file=\"shared.inc\" -->"
	for path, source := range map[string]string{
		childPath:  childSource,
		ownerAPath: ownerSource,
		ownerBPath: ownerSource,
	} {
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ownerAURI := filePathURI(ownerAPath)
	ownerBURI := filePathURI(ownerBPath)
	childURI := filePathURI(childPath)
	writer := newBlockingStatusWriter()
	server := New(strings.NewReader(""), writer, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	server.configureFsGateway()
	server.documents[ownerAURI] = core.NewTextDocument(ownerAURI, "classic-asp", 1, ownerSource)
	server.documents[ownerBURI] = core.NewTextDocument(ownerBURI, "classic-asp", 1, ownerSource)

	first := publicationTestDiagnostic(childURI, "owner-a")
	second := publicationTestDiagnostic(childURI, "owner-b")
	firstDone := make(chan error, 1)
	go func() {
		_, err := server.publishDiagnosticSnapshotContext(context.Background(), ownerAURI, 1, []lsp.Diagnostic{first})
		firstDone <- err
	}()
	select {
	case <-writer.entered:
	case <-time.After(time.Second):
		t.Fatal("first diagnostic publication did not reach the writer")
	}

	secondDone := make(chan error, 1)
	go func() {
		_, err := server.publishDiagnosticSnapshotContext(context.Background(), ownerBURI, 1, []lsp.Diagnostic{second})
		secondDone <- err
	}()
	select {
	case err := <-secondDone:
		t.Fatalf("second publication bypassed the publication barrier: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(writer.release)
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("first diagnostic publication did not complete")
	}
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("second diagnostic publication did not complete")
	}

	messages := publishedDiagnosticMessages(t, []byte(writer.output.String()))
	if len(messages) != 4 {
		t.Fatalf("serialized publication messages = %#v, want two owner clears and two child updates", messages)
	}
	last := messages[len(messages)-1]
	if got := diagnosticMessages(last); len(got) != 2 || got[0] != "owner-a" || got[1] != "owner-b" {
		t.Fatalf("final shared-child aggregate = %#v, want both owners", got)
	}
}

func TestDiagnosticPublicationUsesCapturedOwnerVersionAfterWriteBegins(t *testing.T) {
	root := t.TempDir()
	childPath := filepath.Join(root, "shared.inc")
	ownerPath := filepath.Join(root, "owner.asp")
	ownerSource := "<!-- #include file=\"shared.inc\" -->"
	if err := os.WriteFile(childPath, []byte("<% shared = 1 %>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ownerPath, []byte(ownerSource), 0o600); err != nil {
		t.Fatal(err)
	}
	ownerURI := filePathURI(ownerPath)
	childURI := filePathURI(childPath)
	writer := newBlockingStatusWriter()
	server := New(strings.NewReader(""), writer, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	server.configureFsGateway()
	server.documents[ownerURI] = core.NewTextDocument(ownerURI, "classic-asp", 1, ownerSource)

	done := make(chan error, 1)
	go func() {
		_, err := server.publishDiagnosticSnapshotContext(context.Background(), ownerURI, 1, []lsp.Diagnostic{publicationTestDiagnostic(childURI, "stale-v1")})
		done <- err
	}()
	select {
	case <-writer.entered:
	case <-time.After(time.Second):
		t.Fatal("owner publication did not reach the blocked writer")
	}
	server.mu.Lock()
	server.documents[ownerURI] = core.NewTextDocument(ownerURI, "classic-asp", 2, ownerSource)
	server.mu.Unlock()
	close(writer.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("stale publication did not complete")
	}
	records := publishedDiagnosticRecords(t, []byte(writer.output.String()))
	if len(records) != 2 {
		t.Fatalf("captured owner publication records = %#v, want owner and child", records)
	}
	if records[0].URI != ownerURI || records[0].Version == nil || *records[0].Version != 1 {
		t.Fatalf("captured owner record = %#v, want owner version 1", records[0])
	}
	if records[1].URI != childURI || records[1].Version != nil || diagnosticMessages(records[1].Diagnostics)[0] != "stale-v1" {
		t.Fatalf("captured child record = %#v, want closed child stale-v1", records[1])
	}

	ownerBPath := filepath.Join(root, "owner-b.asp")
	if err := os.WriteFile(ownerBPath, []byte(ownerSource), 0o600); err != nil {
		t.Fatal(err)
	}
	ownerBURI := filePathURI(ownerBPath)
	server.documents[ownerBURI] = core.NewTextDocument(ownerBURI, "classic-asp", 1, ownerSource)
	if _, err := server.publishDiagnosticSnapshotContext(context.Background(), ownerBURI, 1, []lsp.Diagnostic{publicationTestDiagnostic(childURI, "owner-b")}); err != nil {
		t.Fatal(err)
	}
	messages := publishedDiagnosticMessages(t, []byte(writer.output.String()))
	if len(messages) == 0 {
		t.Fatal("owner-b publication produced no diagnostics messages")
	}
	last := messages[len(messages)-1]
	got := diagnosticMessages(last)
	if len(got) != 2 || !containsDiagnosticMessage(got, "stale-v1") || !containsDiagnosticMessage(got, "owner-b") {
		t.Fatalf("owner-b aggregate = %#v, want both committed contributions", got)
	}
}

func TestDiagnosticPublicationUsesCapturedChildVersionAfterWriteBegins(t *testing.T) {
	root := t.TempDir()
	childPath := filepath.Join(root, "shared.inc")
	ownerPath := filepath.Join(root, "owner.asp")
	childSourceV1 := "<% shared = 1 %>"
	childSourceV2 := "<% shared = 2 %>"
	ownerSource := "<!-- #include file=\"shared.inc\" -->"
	if err := os.WriteFile(childPath, []byte(childSourceV1), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ownerPath, []byte(ownerSource), 0o600); err != nil {
		t.Fatal(err)
	}
	ownerURI := filePathURI(ownerPath)
	childURI := filePathURI(childPath)
	writer := newBlockingStatusWriter()
	server := New(strings.NewReader(""), writer, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	server.configureFsGateway()
	server.documents[ownerURI] = core.NewTextDocument(ownerURI, "classic-asp", 1, ownerSource)
	server.documents[childURI] = core.NewTextDocument(childURI, "classic-asp", 1, childSourceV1)

	done := make(chan error, 1)
	go func() {
		_, err := server.publishDiagnosticSnapshotContext(context.Background(), ownerURI, 1, []lsp.Diagnostic{publicationTestDiagnostic(childURI, "stale-child-v1")})
		done <- err
	}()
	select {
	case <-writer.entered:
	case <-time.After(time.Second):
		t.Fatal("owner publication did not reach the blocked writer")
	}
	server.mu.Lock()
	server.documents[childURI] = core.NewTextDocument(childURI, "classic-asp", 2, childSourceV2)
	server.mu.Unlock()
	close(writer.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("stale child publication did not complete")
	}
	records := publishedDiagnosticRecords(t, []byte(writer.output.String()))
	if len(records) != 2 || records[1].URI != childURI || records[1].Version == nil || *records[1].Version != 1 {
		t.Fatalf("captured child publication records = %#v, want child version 1", records)
	}

	if _, err := server.publishDiagnosticSnapshotContext(context.Background(), ownerURI, 1, []lsp.Diagnostic{publicationTestDiagnostic(childURI, "fresh-child-v2")}); err != nil {
		t.Fatal(err)
	}
	records = publishedDiagnosticRecords(t, []byte(writer.output.String()))
	if len(records) == 0 {
		t.Fatal("fresh child publication produced no diagnostics messages")
	}
	last := records[len(records)-1]
	if last.URI != childURI || last.Version == nil || *last.Version != 2 {
		t.Fatalf("fresh child publication = %#v, want child version 2", last)
	}
	if got := diagnosticMessages(last.Diagnostics); len(got) != 1 || got[0] != "fresh-child-v2" {
		t.Fatalf("fresh child aggregate = %#v, want fresh-child-v2 only", got)
	}
}

func TestDiagnosticPublicationUsesCapturedVersionsAfterBatchConstruction(t *testing.T) {
	root := t.TempDir()
	childPath := filepath.Join(root, "shared.inc")
	ownerPath := filepath.Join(root, "owner.asp")
	childSourceV1 := "<% shared = 1 %>"
	childSourceV2 := "<% shared = 2 %>"
	ownerSource := "<!-- #include file=\"shared.inc\" -->"
	if err := os.WriteFile(childPath, []byte(childSourceV1), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ownerPath, []byte(ownerSource), 0o600); err != nil {
		t.Fatal(err)
	}
	ownerURI := filePathURI(ownerPath)
	childURI := filePathURI(childPath)
	var output bytes.Buffer
	server := New(strings.NewReader(""), &output, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	server.configureFsGateway()
	server.documents[ownerURI] = core.NewTextDocument(ownerURI, "classic-asp", 1, ownerSource)
	server.documents[childURI] = core.NewTextDocument(childURI, "classic-asp", 1, childSourceV1)
	batchReady := make(chan struct{})
	releaseBatch := make(chan struct{})
	server.diagnosticPublicationBatchTestHook = func() {
		close(batchReady)
		<-releaseBatch
	}
	done := make(chan error, 1)
	go func() {
		_, err := server.publishDiagnosticSnapshotContext(context.Background(), ownerURI, 1, []lsp.Diagnostic{publicationTestDiagnostic(childURI, "child-v1")})
		done <- err
	}()
	select {
	case <-batchReady:
	case <-time.After(time.Second):
		t.Fatal("diagnostic publication batch was not constructed")
	}
	server.mu.Lock()
	server.documents[childURI] = core.NewTextDocument(childURI, "classic-asp", 2, childSourceV2)
	server.mu.Unlock()
	close(releaseBatch)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("diagnostic publication did not send captured batch")
	}
	records := publishedDiagnosticRecords(t, output.Bytes())
	if len(records) != 2 {
		t.Fatalf("captured batch records = %#v, want owner and child", records)
	}
	if records[0].URI != ownerURI || records[0].Version == nil || *records[0].Version != 1 {
		t.Fatalf("captured owner record = %#v, want version 1", records[0])
	}
	if records[1].URI != childURI || records[1].Version == nil || *records[1].Version != 1 {
		t.Fatalf("captured child record = %#v, want version 1 after child edit", records[1])
	}
}

func TestDiagnosticPublicationRejectsMismatchedChildrenAtomically(t *testing.T) {
	root := t.TempDir()
	ownerPath := filepath.Join(root, "owner.asp")
	childOnePath := filepath.Join(root, "one.inc")
	childTwoPath := filepath.Join(root, "two.inc")
	ownerSource := "<!-- #include file=\"one.inc\" -->\n<!-- #include file=\"two.inc\" -->"
	childOneSource := "<% one = 1 %>"
	childTwoSource := "<% two = 1 %>"
	for path, source := range map[string]string{
		ownerPath:    ownerSource,
		childOnePath: childOneSource,
		childTwoPath: childTwoSource,
	} {
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ownerURI := filePathURI(ownerPath)
	childOneURI := filePathURI(childOnePath)
	childTwoURI := filePathURI(childTwoPath)
	var output bytes.Buffer
	server := New(strings.NewReader(""), &output, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	server.configureFsGateway()
	server.documents[ownerURI] = core.NewTextDocument(ownerURI, "classic-asp", 1, ownerSource)
	server.documents[childOneURI] = core.NewTextDocument(childOneURI, "classic-asp", 1, childOneSource)
	server.documents[childTwoURI] = core.NewTextDocument(childTwoURI, "classic-asp", 1, childTwoSource)
	childOneRevision, ok := server.diagnosticTargetRevisionContext(context.Background(), childOneURI)
	if !ok {
		t.Fatal("failed to capture child-one revision")
	}
	childTwoRevision, ok := server.diagnosticTargetRevisionContext(context.Background(), childTwoURI)
	if !ok {
		t.Fatal("failed to capture child-two revision")
	}
	revisions := map[string]diagnosticTargetRevision{
		workspacepkg.FileIdentityKeyFromURI(childOneURI): childOneRevision,
	}
	if _, err := server.publishDiagnosticSnapshotWithRevisionsContext(context.Background(), ownerURI, 1, []lsp.Diagnostic{publicationTestDiagnostic(childOneURI, "old-one")}, revisions); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	previousItems, previousTargets, previousRevisions := server.publishedDiagnosticContributionSnapshot(ownerURI)
	wrongChildTwoRevision := childTwoRevision
	wrongChildTwoRevision.contentHash = "changed-before-batch"
	revisions[workspacepkg.FileIdentityKeyFromURI(childTwoURI)] = wrongChildTwoRevision
	_, err := server.publishDiagnosticSnapshotWithRevisionsContext(context.Background(), ownerURI, 1, []lsp.Diagnostic{
		publicationTestDiagnostic(childOneURI, "new-one"),
		publicationTestDiagnostic(childTwoURI, "new-two"),
	}, revisions)
	if err != nil {
		t.Fatal(err)
	}
	if records := publishedDiagnosticRecords(t, output.Bytes()); len(records) != 0 {
		t.Fatalf("mismatched child publication sent partial records = %#v", records)
	}
	items, targets, storedRevisions := server.publishedDiagnosticContributionSnapshot(ownerURI)
	if !reflect.DeepEqual(items, previousItems) || !reflect.DeepEqual(targets, previousTargets) || !reflect.DeepEqual(storedRevisions, previousRevisions) {
		t.Fatalf("mismatched child publication changed bookkeeping: before=(%#v,%#v,%#v), after=(%#v,%#v,%#v)", previousItems, previousTargets, previousRevisions, items, targets, storedRevisions)
	}
	revisions[workspacepkg.FileIdentityKeyFromURI(childTwoURI)] = childTwoRevision
	if _, err := server.publishDiagnosticSnapshotWithRevisionsContext(context.Background(), ownerURI, 1, []lsp.Diagnostic{
		publicationTestDiagnostic(childOneURI, "new-one"),
		publicationTestDiagnostic(childTwoURI, "new-two"),
	}, revisions); err != nil {
		t.Fatal(err)
	}
	records := publishedDiagnosticRecords(t, output.Bytes())
	if len(records) != 3 {
		t.Fatalf("recomputed publication records = %#v, want owner and two children", records)
	}
	for _, record := range records {
		if record.URI == childOneURI && (record.Version == nil || *record.Version != 1 || diagnosticMessages(record.Diagnostics)[0] != "new-one") {
			t.Fatalf("recomputed child-one record = %#v", record)
		}
		if record.URI == childTwoURI && (record.Version == nil || *record.Version != 1 || diagnosticMessages(record.Diagnostics)[0] != "new-two") {
			t.Fatalf("recomputed child-two record = %#v", record)
		}
	}
}

func TestDiagnosticPublicationFailureKeepsBookkeepingAndRetries(t *testing.T) {
	server, ownerURI, childURI, writer := newDiagnosticFailureFixture(t)
	writer.failAt = 1
	writer.failAlways = true
	writer.failErr = errors.New("injected first publication failure")
	if _, err := server.publishDiagnosticSnapshotContext(context.Background(), ownerURI, 1, []lsp.Diagnostic{publicationTestDiagnostic(childURI, "first")}); err == nil {
		t.Fatal("first publication unexpectedly succeeded")
	}
	if writer.output.Len() != 0 {
		t.Fatalf("first failed publication escaped bytes = %d, want zero", writer.output.Len())
	}
	items, targets, revisions := server.publishedDiagnosticContributionSnapshot(ownerURI)
	if len(items) != 0 || len(targets) != 0 || len(revisions) != 0 {
		t.Fatalf("first failed publication changed bookkeeping: items=%#v targets=%#v revisions=%#v", items, targets, revisions)
	}
	server, ownerURI, childURI, writer = newDiagnosticFailureFixture(t)
	if _, err := server.publishDiagnosticSnapshotContext(context.Background(), ownerURI, 1, []lsp.Diagnostic{publicationTestDiagnostic(childURI, "retry")}); err != nil {
		t.Fatal(err)
	}
	records := publishedDiagnosticRecords(t, writer.output.Bytes())
	if len(records) != 2 || records[1].URI != childURI || diagnosticMessages(records[1].Diagnostics)[0] != "retry" {
		t.Fatalf("successful retry records = %#v, want owner and retry child", records)
	}
}

func TestDiagnosticPublicationRepeatedZeroWriteStopsBeforeCompensation(t *testing.T) {
	server, ownerURI, childURI, writer := newDiagnosticFailureFixture(t)
	if _, err := server.publishDiagnosticSnapshotContext(context.Background(), ownerURI, 1, []lsp.Diagnostic{publicationTestDiagnostic(childURI, "old")}); err != nil {
		t.Fatal(err)
	}
	previousItems, previousTargets, previousRevisions := server.publishedDiagnosticContributionSnapshot(ownerURI)
	writer.output.Reset()
	writer.failAt = writer.calls + 2
	writer.failAlways = true
	writer.failErr = errors.New("injected second publication failure")
	_, err := server.publishDiagnosticSnapshotContext(context.Background(), ownerURI, 1, []lsp.Diagnostic{publicationTestDiagnostic(childURI, "new")})
	var fatal *rpcTransportFatalError
	if !errors.As(err, &fatal) {
		t.Fatalf("second publication error = %v, want fatal transport error", err)
	}
	items, targets, revisions := server.publishedDiagnosticContributionSnapshot(ownerURI)
	if !reflect.DeepEqual(items, previousItems) || !reflect.DeepEqual(targets, previousTargets) || !reflect.DeepEqual(revisions, previousRevisions) {
		t.Fatalf("second failed publication changed bookkeeping: before=(%#v,%#v,%#v), after=(%#v,%#v,%#v)", previousItems, previousTargets, previousRevisions, items, targets, revisions)
	}
	records := publishedDiagnosticRecords(t, writer.output.Bytes())
	for _, record := range records {
		if record.URI == childURI {
			t.Fatalf("failed second publication escaped child notification = %#v", record)
		}
	}
}

func TestDiagnosticPublicationZeroWriteRetriesAndCommits(t *testing.T) {
	server, ownerURI, childURI, writer := newDiagnosticFailureFixture(t)
	writer.shortAt = 1
	if _, err := server.publishDiagnosticSnapshotContext(context.Background(), ownerURI, 1, []lsp.Diagnostic{publicationTestDiagnostic(childURI, "zero-retry")}); err != nil {
		t.Fatalf("zero-write publication = %v, want success after retry", err)
	}
	records := publishedDiagnosticRecords(t, writer.output.Bytes())
	if len(records) != 2 || records[len(records)-1].URI != childURI || diagnosticMessages(records[len(records)-1].Diagnostics)[0] != "zero-retry" {
		t.Fatalf("zero-write retry records = %#v, want successful child publication", records)
	}
}

type diagnosticFailureWriter struct {
	output     bytes.Buffer
	calls      int
	failAt     int
	failAlways bool
	failErr    error
	shortAt    int
}

func (w *diagnosticFailureWriter) Write(data []byte) (int, error) {
	w.calls++
	if w.shortAt == w.calls {
		return 0, nil
	}
	if w.failAt == w.calls || (w.failAlways && w.failAt > 0 && w.calls > w.failAt) {
		return 0, w.failErr
	}
	return w.output.Write(data)
}

func newDiagnosticFailureFixture(t *testing.T) (*Server, string, string, *diagnosticFailureWriter) {
	t.Helper()
	root := t.TempDir()
	childPath := filepath.Join(root, "shared.inc")
	ownerPath := filepath.Join(root, "owner.asp")
	ownerSource := "<!-- #include file=\"shared.inc\" -->"
	if err := os.WriteFile(childPath, []byte("<% shared = 1 %>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ownerPath, []byte(ownerSource), 0o600); err != nil {
		t.Fatal(err)
	}
	ownerURI := filePathURI(ownerPath)
	childURI := filePathURI(childPath)
	writer := &diagnosticFailureWriter{}
	server := New(strings.NewReader(""), writer, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	server.configureFsGateway()
	server.documents[ownerURI] = core.NewTextDocument(ownerURI, "classic-asp", 1, ownerSource)
	server.documents[childURI] = core.NewTextDocument(childURI, "classic-asp", 1, "<% shared = 1 %>")
	return server, ownerURI, childURI, writer
}

func publicationTestDiagnostic(uri, message string) lsp.Diagnostic {
	return lsp.Diagnostic{
		Range:   lsp.Range{Start: lsp.Position{Line: 0}, End: lsp.Position{Line: 0, Character: 1}},
		Source:  "publication-test",
		Message: message,
		Data:    map[string]any{"uri": uri},
	}
}

type publishedDiagnosticRecord struct {
	URI         string
	Version     *int
	Diagnostics []lsp.Diagnostic
}

func publishedDiagnosticRecords(t *testing.T, raw []byte) []publishedDiagnosticRecord {
	t.Helper()
	reader := bufio.NewReader(bytes.NewReader(raw))
	var records []publishedDiagnosticRecord
	for {
		message, err := readMessage(reader)
		if err == io.EOF {
			return records
		}
		if err != nil {
			t.Fatal(err)
		}
		if message.Method != "textDocument/publishDiagnostics" {
			continue
		}
		var params struct {
			URI         string           `json:"uri"`
			Version     *int             `json:"version"`
			Diagnostics []lsp.Diagnostic `json:"diagnostics"`
		}
		if err := json.Unmarshal(message.Params, &params); err != nil {
			t.Fatal(err)
		}
		records = append(records, publishedDiagnosticRecord{URI: params.URI, Version: params.Version, Diagnostics: params.Diagnostics})
	}
}

func publishedDiagnosticMessages(t *testing.T, raw []byte) [][]lsp.Diagnostic {
	t.Helper()
	reader := bufio.NewReader(bytes.NewReader(raw))
	var messages [][]lsp.Diagnostic
	for {
		message, err := readMessage(reader)
		if err == io.EOF {
			return messages
		}
		if err != nil {
			t.Fatal(err)
		}
		if message.Method != "textDocument/publishDiagnostics" {
			continue
		}
		var params struct {
			Diagnostics []lsp.Diagnostic `json:"diagnostics"`
		}
		if err := json.Unmarshal(message.Params, &params); err != nil {
			t.Fatal(err)
		}
		messages = append(messages, params.Diagnostics)
	}
}

func diagnosticMessages(diagnostics []lsp.Diagnostic) []string {
	messages := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		messages = append(messages, diagnostic.Message)
	}
	return messages
}

func containsDiagnosticMessage(messages []string, wanted string) bool {
	for _, message := range messages {
		if message == wanted {
			return true
		}
	}
	return false
}
